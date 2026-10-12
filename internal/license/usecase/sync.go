package usecase

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// syncer propagates a subscription change to the derived entitlements: the
// entitlements table, an entitlement.updated event and, once the
// transaction commits, the shared cache entry.
type syncer struct {
	entitlements EntitlementRepository
	events       EventPublisher
	cache        EntitlementCache
}

func (s syncer) entitlementsChanged(ctx context.Context, sub *domain.Subscription) error {
	if err := s.entitlements.Replace(ctx, sub.UserID, domain.FeatureFlags(sub)); err != nil {
		return err
	}

	err := s.events.Publish(ctx, EventEntitlementUpdated, "", sub.UserID, map[string]any{
		keyUserID:   sub.UserID,
		keyStatus:   sub.Status,
		"plan_code": sub.Plan.Code,
	})
	if err != nil {
		return err
	}

	userID := sub.UserID

	platformdb.AfterCommit(ctx, func() { s.invalidate(context.WithoutCancel(ctx), userID) })

	return nil
}

// statusChanged publishes subscription.status_changed; extra adds fields to
// the payload (the expiry job adds the payment details of the e-mail the
// owner gets when the subscription lapses).
func (s syncer) statusChanged(
	ctx context.Context, sub *domain.Subscription, from domain.SubscriptionStatus, extra map[string]any,
) error {
	payload := map[string]any{
		"subscription_id":    sub.ID,
		keyUserID:            sub.UserID,
		"from":               from,
		"to":                 sub.Status,
		"current_period_end": sub.CurrentPeriodEnd.Format(time.RFC3339),
	}

	maps.Copy(payload, extra)

	return s.events.Publish(ctx, EventSubscriptionStatusChanged, "", sub.UserID, payload)
}

// invalidate drops a cache entry. A failure only delays the change until
// the entry's TTL expires, so it is logged rather than returned.
func (s syncer) invalidate(ctx context.Context, userID string) {
	if err := s.cache.Delete(ctx, userID); err != nil {
		slog.Warn("license: invalidating the entitlement cache failed", "user_id", userID, "error", err)
	}
}
