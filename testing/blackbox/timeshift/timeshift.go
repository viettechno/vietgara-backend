// Package timeshift moves stored dates so that time-dependent rules can be
// checked without waiting days: the SQL helpers H1 to H4 of the E2E
// scenarios' README (section 3.4). They are setup only; every expected
// result is still read through the public API or the test inbox. They need
// direct database access, so they run on the stubbed target and skip
// elsewhere.
package timeshift

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
)

var errNoRows = errors.New("no row matched")

// EndPeriod is H1: the owner's current period (trial or paid) ended one
// minute ago. The expiry job then makes the subscription past due.
func EndPeriod(tb testing.TB, ownerEmail string) {
	tb.Helper()

	exec(tb, `UPDATE subscriptions SET current_period_end = now() - interval '1 minute'
		WHERE user_id = (SELECT id FROM users WHERE email = $1)`, ownerEmail)
}

// EndGrace is H2: the grace period ended one minute ago. The expiry job
// then suspends the subscription.
func EndGrace(tb testing.TB, ownerEmail string) {
	tb.Helper()

	exec(tb, `UPDATE subscriptions SET grace_period_end = now() - interval '1 minute'
		WHERE user_id = (SELECT id FROM users WHERE email = $1)`, ownerEmail)
}

// ExpireSoon is H3: the period ends in three days and the reminder state
// is cleared, so the expiry job sends the "expiring soon" reminder.
func ExpireSoon(tb testing.TB, ownerEmail string) {
	tb.Helper()

	exec(tb, `UPDATE subscriptions SET current_period_end = now() + interval '3 days', reminder_period_end = NULL
		WHERE user_id = (SELECT id FROM users WHERE email = $1)`, ownerEmail)
}

// ExpireInvitation is H4: the invitee's pending invitation expired one
// minute ago.
func ExpireInvitation(tb testing.TB, inviteeEmail string) {
	tb.Helper()

	exec(tb, `UPDATE staff_invitations SET expires_at = now() - interval '1 minute'
		WHERE status = 'pending' AND user_id = (SELECT id FROM users WHERE email = $1)`, inviteeEmail)
}

func exec(tb testing.TB, sql, arg string) {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.DatabaseURL == "" {
		tb.Skipf("needs direct database access to move stored dates (stubbed target only); running against %s", cfg.Target)
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		tb.Fatalf("connect to the stubbed database (BLACKBOX_DATABASE_URL): %v", err)
	}

	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx, sql, arg)
	if err != nil {
		tb.Fatalf("shift time for %s: %v", arg, err)
	}

	if tag.RowsAffected() == 0 {
		tb.Fatalf("shift time for %s: %v", arg, fmt.Errorf("%w", errNoRows))
	}
}
