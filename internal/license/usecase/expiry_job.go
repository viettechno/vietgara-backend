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
	payments      PaymentRepository
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
	payments PaymentRepository,
	tx Transactor,
	opts ...Option,
) *ExpiryJob {
	return &ExpiryJob{
		subscriptions: subscriptions,
		payments:      payments,
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

	if err := j.remindReviewers(ctx, now); err != nil {
		errs = append(errs, err)
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
			payload, err := j.expiringSoon(ctx, sub)
			if err != nil {
				return err
			}

			if err := j.sync.events.Publish(ctx, EventSubscriptionExpiringSoon, "", userID, payload); err != nil {
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

// expiringSoon is the payload of the renewal reminder: what to pay and
// where, so the owner can transfer straight away (FR-LIC-04, FR-LIC-07).
func (j *ExpiryJob) expiringSoon(ctx context.Context, sub *domain.Subscription) (map[string]any, error) {
	settings, err := j.payments.Settings(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"subscription_id":    sub.ID,
		keyUserID:            sub.UserID,
		keyStatus:            sub.Status,
		"plan_code":          sub.Plan.Code,
		"current_period_end": sub.CurrentPeriodEnd.Format(time.RFC3339),
		keyAmountDue:         sub.Plan.Price - min(sub.CreditBalance, sub.Plan.Price),
		"bank_name":          settings.BankName,
		"account_number":     settings.AccountNumber,
		"account_holder":     settings.AccountHolder,
		"branch":             settings.Branch,
	}, nil
}

// reviewReminderInterval is how often the admins are reminded of requests
// waiting for review.
const reviewReminderInterval = 20 * time.Hour

// remindReviewers publishes subscription.payment_review_reminder once a day
// while requests wait for review (LLD 3.2); the claim makes it once across
// instances.
func (j *ExpiryJob) remindReviewers(ctx context.Context, now time.Time) error {
	return j.tx.WithTransaction(ctx, platformdb.Scope{}, func(ctx context.Context) error {
		waiting, oldest, err := j.payments.WaitingForReview(ctx)
		if err != nil || waiting == 0 {
			return err
		}

		claimed, err := j.payments.ClaimReviewReminder(ctx, now, reviewReminderInterval)
		if err != nil || !claimed {
			return err
		}

		return j.sync.events.Publish(ctx, EventPaymentReviewReminder, "", "", map[string]any{
			"waiting": waiting, "oldest_submitted_at": oldest.Format(time.RFC3339),
		})
	})
}
