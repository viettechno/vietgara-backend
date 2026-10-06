package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/testaccounts"
)

// Test account purge defaults.
const (
	DefaultTestAccountRetention     = 24 * time.Hour
	DefaultTestAccountPurgeInterval = time.Hour
)

// TestAccountDeleter deletes test accounts and, by cascade, everything
// they own (garages and their data, staff memberships, subscriptions).
type TestAccountDeleter interface {
	// DeleteTestAccounts deletes the accounts whose e-mail is on
	// emailDomain and that were created before createdBefore, returning
	// how many were deleted.
	DeleteTestAccounts(ctx context.Context, emailDomain string, createdBefore time.Time) (int64, error)
}

// TestAccountPurgeJob deletes test accounts older than the retention, so
// the automated suite's sign-ups do not pile up on a shared deployment.
type TestAccountPurgeJob struct {
	accounts  TestAccountDeleter
	domain    testaccounts.Domain
	retention time.Duration
	now       func() time.Time
}

// NewTestAccountPurgeJob wires the purge job for the accounts on domain.
func NewTestAccountPurgeJob(accounts TestAccountDeleter, domain testaccounts.Domain, retention time.Duration) *TestAccountPurgeJob {
	return &TestAccountPurgeJob{accounts: accounts, domain: domain, retention: retention, now: time.Now}
}

// WithClock replaces time.Now (tests).
func (j *TestAccountPurgeJob) WithClock(now func() time.Time) *TestAccountPurgeJob {
	j.now = now

	return j
}

// Schedule runs the job now and then every interval until ctx is cancelled.
func (j *TestAccountPurgeJob) Schedule(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := j.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("identity: test account purge failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Run deletes the test accounts created more than the retention ago. It
// does nothing for a zero domain.
func (j *TestAccountPurgeJob) Run(ctx context.Context) error {
	if j.domain.IsZero() {
		return nil
	}

	deleted, err := j.accounts.DeleteTestAccounts(ctx, j.domain.String(), j.now().Add(-j.retention))
	if err != nil {
		return fmt.Errorf("delete test accounts: %w", err)
	}

	if deleted > 0 {
		slog.InfoContext(ctx, "identity: purged test accounts", "count", deleted, "domain", j.domain.String())
	}

	return nil
}
