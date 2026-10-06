package usecase_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/testaccounts"
)

type fakeDeleter struct {
	domain  string
	before  time.Time
	calls   atomic.Int32
	deleted int64
	err     error
}

func (f *fakeDeleter) DeleteTestAccounts(_ context.Context, emailDomain string, createdBefore time.Time) (int64, error) {
	f.calls.Add(1)
	f.domain, f.before = emailDomain, createdBefore

	return f.deleted, f.err
}

func TestTestAccountPurgeJob(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	domain, err := testaccounts.Parse("blackbox.vietgara.test")
	require.NoError(t, err)

	t.Run("deletes accounts older than the retention", func(t *testing.T) {
		t.Parallel()

		deleter := &fakeDeleter{deleted: 3}
		job := usecase.NewTestAccountPurgeJob(deleter, domain, 24*time.Hour).WithClock(func() time.Time { return now })

		require.NoError(t, job.Run(t.Context()))
		assert.Equal(t, "blackbox.vietgara.test", deleter.domain)
		assert.Equal(t, now.Add(-24*time.Hour), deleter.before)
	})

	t.Run("reports a failed deletion", func(t *testing.T) {
		t.Parallel()

		deleter := &fakeDeleter{deleted: 1, err: errors.New("invited_by")}
		job := usecase.NewTestAccountPurgeJob(deleter, domain, time.Hour)

		require.ErrorContains(t, job.Run(t.Context()), "invited_by")
	})

	t.Run("does nothing without a domain", func(t *testing.T) {
		t.Parallel()

		deleter := &fakeDeleter{}
		job := usecase.NewTestAccountPurgeJob(deleter, testaccounts.Domain{}, time.Hour)

		require.NoError(t, job.Run(t.Context()))
		assert.Zero(t, deleter.calls.Load())
	})

	t.Run("schedule runs at once and stops with its context", func(t *testing.T) {
		t.Parallel()

		deleter := &fakeDeleter{err: errors.New("db down")}
		job := usecase.NewTestAccountPurgeJob(deleter, domain, time.Hour)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			job.Schedule(ctx, time.Hour)
			close(done)
		}()

		require.Eventually(t, func() bool { return deleter.calls.Load() > 0 }, time.Second, 5*time.Millisecond)
		cancel()
		<-done
	})
}
