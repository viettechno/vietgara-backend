package usecase

import (
	"context"
	"log/slog"
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
		keyPlanCode: sub.Plan.Code,
	})
	if err != nil {
		return err
	}

	userID := sub.UserID

	platformdb.AfterCommit(ctx, func() { s.invalidate(context.WithoutCancel(ctx), userID) })

	return nil
}

func (s syncer) statusChanged(ctx context.Context, sub *domain.Subscription, from domain.SubscriptionStatus) error {
	return s.events.Publish(ctx, EventSubscriptionStatusChanged, "", sub.UserID, map[string]any{
		keySubscriptionID:   sub.ID,
		keyUserID:           sub.UserID,
		"from":              from,
		"to":                sub.Status,
		keyCurrentPeriodEnd: sub.CurrentPeriodEnd.Format(time.RFC3339),
	})
}

// invalidate drops a cache entry. A failure only delays the change until
// the entry's TTL expires, so it is logged rather than returned.
func (s syncer) invalidate(ctx context.Context, userID string) {
	if err := s.cache.Delete(ctx, userID); err != nil {
		slog.Warn("license: invalidating the entitlement cache failed", "user_id", userID, "error", err)
	}
}
