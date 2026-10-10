package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

// Test user purge defaults.
const (
	DefaultTestUserRetention     = 24 * time.Hour
	DefaultTestUserPurgeInterval = time.Hour
)

// TestUserDeleter deletes test users and, by cascade, everything
// they own (garages and their data, staff memberships, subscriptions).
type TestUserDeleter interface {
	// DeleteTestUsers deletes the users whose e-mail is on
	// emailDomain and that were created before createdBefore, returning
	// how many were deleted.
	DeleteTestUsers(ctx context.Context, emailDomain string, createdBefore time.Time) (int64, error)
}

// TestUserPurgeJob deletes test users older than the retention, so
// the automated suite's sign-ups do not pile up on a shared deployment.
type TestUserPurgeJob struct {
	users     TestUserDeleter
	domain    testusers.Domain
	retention time.Duration
	now       func() time.Time
}

// NewTestUserPurgeJob wires the purge job for the users on domain.
func NewTestUserPurgeJob(users TestUserDeleter, domain testusers.Domain, retention time.Duration) *TestUserPurgeJob {
	return &TestUserPurgeJob{users: users, domain: domain, retention: retention, now: time.Now}
}

// WithClock replaces time.Now (tests).
func (j *TestUserPurgeJob) WithClock(now func() time.Time) *TestUserPurgeJob {
	j.now = now

	return j
}

// Schedule runs the job now and then every interval until ctx is cancelled.
func (j *TestUserPurgeJob) Schedule(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := j.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("identity: test user purge failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Run deletes the test users created more than the retention ago. It
// does nothing for a zero domain.
func (j *TestUserPurgeJob) Run(ctx context.Context) error {
	if j.domain.IsZero() {
		return nil
	}

	deleted, err := j.users.DeleteTestUsers(ctx, j.domain.String(), j.now().Add(-j.retention))
	if err != nil {
		return fmt.Errorf("delete test users: %w", err)
	}

	if deleted > 0 {
		slog.InfoContext(ctx, "identity: purged test users", "count", deleted, "domain", j.domain.String())
	}

	return nil
}
