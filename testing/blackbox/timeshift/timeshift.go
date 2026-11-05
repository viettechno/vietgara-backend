// Package timeshift moves stored dates so that time-dependent rules can be
// checked without waiting days: the SQL helpers H1 to H6 of the E2E
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

// ExpireGrant is H5: the staff member's approved support grant ran out one
// minute ago.
func ExpireGrant(tb testing.TB, staffEmail string) {
	tb.Helper()

	exec(tb, `UPDATE support_access_grants SET expires_at = now() - interval '1 minute'
		WHERE approved_at IS NOT NULL AND revoked_at IS NULL
		AND staff_user_id = (SELECT id FROM users WHERE email = $1)`, staffEmail)
}

// AgeRequest is H6: the staff member's support request has waited eight
// days for the owner, a day longer than a request lives.
func AgeRequest(tb testing.TB, staffEmail string) {
	tb.Helper()

	exec(tb, `UPDATE support_access_grants SET requested_at = now() - interval '8 days'
		WHERE approved_at IS NULL AND revoked_at IS NULL
		AND staff_user_id = (SELECT id FROM users WHERE email = $1)`, staffEmail)
}

// AgeReceivable is the receivable helper: the due date of the receivable of
// the settlement moves to a number of days in the past (garage time is not needed: the
// date is compared with today in the garage's zone, so a day more or less
// at midnight does not matter to the offsets the scenarios use). The reminder
// job then finds it overdue.
func AgeReceivable(tb testing.TB, settlementID string, days int) {
	tb.Helper()

	exec(tb, `UPDATE receivables SET due_date = (now() AT TIME ZONE 'Asia/Ho_Chi_Minh')::date - ($2::int)
		WHERE settlement_id = $1::uuid`, settlementID, days)
}

// AgeLedger moves every ledger entry of the receivable of the settlement a
// number of days into the past, as if the settlement had been issued and paid
// before the period a reconciliation looks at.
func AgeLedger(tb testing.TB, settlementID string, days int) {
	tb.Helper()

	exec(tb, `UPDATE receivable_entries SET occurred_at = occurred_at - ($2::int * interval '1 day')
		WHERE receivable_id IN (SELECT id FROM receivables WHERE settlement_id = $1::uuid)`, settlementID, days)
}

// ClearReceivableCustomer removes the customer from the receivable of the
// settlement, as when the customer could not be found.
func ClearReceivableCustomer(tb testing.TB, settlementID string) {
	tb.Helper()

	exec(tb, `UPDATE receivables SET customer_id = NULL WHERE settlement_id = $1::uuid`, settlementID)
}

func exec(tb testing.TB, sql string, args ...any) {
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

	tag, err := conn.Exec(ctx, sql, args...)
	if err != nil {
		tb.Fatalf("shift time for %v: %v", args, err)
	}

	if tag.RowsAffected() == 0 {
		tb.Fatalf("shift time for %v: %v", args, fmt.Errorf("%w", errNoRows))
	}
}
