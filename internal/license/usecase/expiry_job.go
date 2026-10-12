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
// period) and then suspended (FR-LIC-04). It also applies the plan change an
// owner scheduled once it is due (FR-LIC-02): a change whose new plan the
// owner's usage still exceeds is postponed, with a notice, and tried again
// on the next run.
//
// Every subscription is handled in its own transaction holding its row
// lock and re-evaluated after locking, so a concurrent plan change or a
// second instance running the job cannot double-publish or undo a change.
type ExpiryJob struct {
	subscriptions SubscriptionRepository
	usage         UsageReader
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
	usage UsageReader,
	tx Transactor,
	opts ...Option,
) *ExpiryJob {
	return &ExpiryJob{
		subscriptions: subscriptions,
		usage:         usage,
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

		applied, err := j.applyDueChange(ctx, sub, now)
		if err != nil {
			return err
		}

		if applied {
			// The change started a new period: nothing else is due.
			return nil
		}

		changed := false

		if sub.NeedsExpiryReminder(now, domain.ExpiringSoonWindow) {
			err := j.sync.events.Publish(ctx, EventSubscriptionExpiringSoon, "", userID, map[string]any{
				keySubscriptionID:   sub.ID,
				keyUserID:           userID,
				keyStatus:           sub.Status,
				keyPlanCode:         sub.Plan.Code,
				keyCurrentPeriodEnd: sub.CurrentPeriodEnd.Format(time.RFC3339),
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

// applyDueChange applies the subscription's pending plan change when it is
// due and the new plan's limits hold, and postpones it (once, with a
// notice) when they do not. It reports whether the plan changed.
func (j *ExpiryJob) applyDueChange(ctx context.Context, sub *domain.Subscription, now time.Time) (bool, error) {
	change := sub.PendingChange
	if change == nil || !change.Due(now) {
		return false, nil
	}

	garages, staff, err := j.usage.OwnerUsage(ctx, sub.UserID)
	if err != nil {
		return false, err
	}

	if plan := change.Plan; garages > plan.MaxGarages || staff > plan.MaxStaff {
		return false, j.postpone(ctx, sub, change, garages, staff, now)
	}

	// A change held back earlier starts when the owner's usage fits again.
	start := change.EffectiveAt
	if change.Status == domain.PlanChangePostponed {
		start = now
	}

	fromPlan, from := sub.PlanID, sub.Status

	if err := sub.ChangePlan(change.Plan, start); err != nil {
		return false, err
	}

	change.Status, change.AppliedAt = domain.PlanChangeApplied, &now

	if err := j.subscriptions.SaveChange(ctx, change); err != nil {
		return false, err
	}

	if err := j.subscriptions.Update(ctx, sub); err != nil {
		return false, err
	}

	sub.PendingChange = nil

	if from != sub.Status {
		if err := j.sync.statusChanged(ctx, sub, from); err != nil {
			return false, err
		}
	}

	err = j.sync.events.Publish(ctx, EventPlanChangeApplied, "", sub.UserID, map[string]any{
		keySubscriptionID: sub.ID, keyUserID: sub.UserID, "from_plan_id": fromPlan, "plan_id": sub.PlanID,
		keyPlanCode: sub.Plan.Code, keyCurrentPeriodEnd: sub.CurrentPeriodEnd.Format(time.RFC3339),
	})
	if err != nil {
		return false, err
	}

	return true, j.sync.entitlementsChanged(ctx, sub)
}

// postpone holds a due change back because the owner's usage exceeds the
// new plan, and tells the owner once.
func (j *ExpiryJob) postpone(
	ctx context.Context, sub *domain.Subscription, change *domain.PlanChange, garages, staff int, now time.Time,
) error {
	if change.Status == domain.PlanChangePostponed {
		return nil
	}

	change.Status, change.PostponedAt = domain.PlanChangePostponed, &now

	if err := j.subscriptions.SaveChange(ctx, change); err != nil {
		return err
	}

	return j.sync.events.Publish(ctx, EventPlanChangePostponed, "", sub.UserID, map[string]any{
		keySubscriptionID: sub.ID, keyUserID: sub.UserID, keyPlanCode: change.Plan.Code,
		"max_garages": change.Plan.MaxGarages, "max_staff": change.Plan.MaxStaff, "garages": garages, "staff": staff,
	})
}
