package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// DefaultExpiryJobInterval is how often the expiry job runs.
const DefaultExpiryJobInterval = time.Hour

// ExpiryJob is the subscription expiry check of LLD Section 3.2: it
// publishes subscription.expiring_soon ahead of the end of a trial/paid
// period and moves expired subscriptions to past_due (read-only grace
// period) and then suspended (FR-LIC-04).
//
// Every subscription is handled in its own transaction holding its row
// lock and re-evaluated after locking, so a concurrent plan change or a
// second instance running the job cannot double-publish or undo a change.
type ExpiryJob struct {
	subscriptions SubscriptionRepository
	tx            Transactor
	sync          syncer
	opts          options
}

// NewExpiryJob wires the expiry job.
func NewExpiryJob(
	subscriptions SubscriptionRepository,
	entitlements EntitlementRepository,
	events EventPublisher,
	cache EntitlementCache,
	tx Transactor,
	opts ...Option,
) *ExpiryJob {
	return &ExpiryJob{
		subscriptions: subscriptions,
		tx:            tx,
		sync:          syncer{entitlements: entitlements, events: events, cache: cache},
		opts:          newOptions(opts),
	}
}

// Schedule runs the job now and then every interval until ctx is cancelled.
func (j *ExpiryJob) Schedule(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := j.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("license: subscription expiry job failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Run checks every subscription whose period ends within the reminder
// window. A failure on one user does not stop the others; all failures
// are returned together.
func (j *ExpiryJob) Run(ctx context.Context) error {
	now := j.opts.now().UTC()

	userIDs, err := j.subscriptions.ListUserIDsDueBefore(ctx, now.Add(domain.ExpiringSoonWindow))
	if err != nil {
		return err
	}

	var errs []error

	for _, userID := range userIDs {
		if err := j.process(ctx, userID, now); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (j *ExpiryJob) process(ctx context.Context, userID string, now time.Time) error {
	return j.tx.WithTransaction(ctx, platformdb.UserScope(userID), func(ctx context.Context) error {
		sub, err := j.subscriptions.LockByUserID(ctx, userID)
		if err != nil {
			return err
		}

		changed := false

		if sub.NeedsExpiryReminder(now, domain.ExpiringSoonWindow) {
			err := j.sync.events.Publish(ctx, EventSubscriptionExpiringSoon, "", userID, map[string]any{
				"subscription_id":    sub.ID,
				keyUserID:            userID,
				"status":             sub.Status,
				"plan_code":          sub.Plan.Code,
				"current_period_end": sub.CurrentPeriodEnd.Format(time.RFC3339),
			})
			if err != nil {
				return err
			}

			sub.MarkReminded()

			changed = true
		}

		from := sub.Status
		if sub.Expire(now, j.opts.grace) {
			changed = true
		}

		if !changed {
			return nil
		}

		if err := j.subscriptions.Update(ctx, sub); err != nil {
			return err
		}

		if from == sub.Status {
			return nil
		}

		if err := j.sync.statusChanged(ctx, sub, from); err != nil {
			return err
		}

		return j.sync.entitlementsChanged(ctx, sub)
	})
}
