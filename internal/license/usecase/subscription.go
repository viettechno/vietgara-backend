package usecase

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// Usage is what an owner currently uses of the plan limits.
type Usage struct {
	Garages           int
	Staff             int
	TransactionsToday int
}

// SubscriptionUseCase (FR-LIC-01/02/05) manages owners' subscriptions and
// the plan catalogue.
type SubscriptionUseCase struct {
	plans         PlanRepository
	subscriptions SubscriptionRepository
	usage         UsageReader
	counter       TransactionCounter
	audit         AuditLogger
	sync          syncer
	opts          options
}

// SubscriptionDeps are the dependencies of SubscriptionUseCase.
type SubscriptionDeps struct {
	Plans         PlanRepository
	Subscriptions SubscriptionRepository
	Entitlements  EntitlementRepository
	Usage         UsageReader
	Counter       TransactionCounter
	Events        EventPublisher
	Cache         EntitlementCache
	Audit         AuditLogger
}

// NewSubscriptionUseCase wires the subscription use case.
func NewSubscriptionUseCase(deps SubscriptionDeps, opts ...Option) *SubscriptionUseCase {
	return &SubscriptionUseCase{
		plans:         deps.Plans,
		subscriptions: deps.Subscriptions,
		usage:         deps.Usage,
		counter:       deps.Counter,
		audit:         deps.Audit,
		sync:          syncer{entitlements: deps.Entitlements, events: deps.Events, cache: deps.Cache},
		opts:          newOptions(opts),
	}
}

// EnsureTrial starts the 14-day trial of an owner (FR-LIC-01) unless they
// already have a subscription. Idempotent: called when the owner's e-mail
// is verified and whenever a limit is checked for an owner without one.
func (uc *SubscriptionUseCase) EnsureTrial(ctx context.Context, userID string) error {
	plan, err := uc.plans.FindByCode(ctx, domain.TrialPlanCode)
	if err != nil {
		return toAppError("find trial plan", err)
	}

	sub := domain.NewTrial(userID, plan, uc.opts.now().UTC())

	inserted, err := uc.subscriptions.CreateIfAbsent(ctx, sub)
	if err != nil {
		return toAppError("create trial subscription", err)
	}

	if !inserted {
		return nil
	}

	if err := uc.sync.entitlementsChanged(ctx, sub); err != nil {
		return toAppError("sync entitlements", err)
	}

	return nil
}

// HandleUserEmailVerified is the user.email_verified consumer: every
// verified owner starts a trial.
func (uc *SubscriptionUseCase) HandleUserEmailVerified(ctx context.Context, event outbox.Event) error {
	var payload struct {
		UserID string `json:"user_id"`
	}

	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.UserID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.EnsureTrial(ctx, payload.UserID)
}

// ListPlans returns the plans owners may choose, cheapest first.
func (uc *SubscriptionUseCase) ListPlans(ctx context.Context) ([]*domain.Plan, error) {
	plans, err := uc.plans.ListPublic(ctx)
	if err != nil {
		return nil, toAppError("list plans", err)
	}

	return plans, nil
}

// GetMine returns the owner's subscription (starting the trial if needed)
// and current usage.
func (uc *SubscriptionUseCase) GetMine(ctx context.Context, userID string) (*domain.Subscription, Usage, error) {
	sub, err := uc.subscriptions.FindByUserID(ctx, userID)
	if errors.Is(err, domain.ErrSubscriptionNotFound) {
		if err := uc.EnsureTrial(ctx, userID); err != nil {
			return nil, Usage{}, err
		}

		sub, err = uc.subscriptions.FindByUserID(ctx, userID)
	}

	if err != nil {
		return nil, Usage{}, toAppError("get subscription", err)
	}

	usage, err := uc.Usage(ctx, userID)
	if err != nil {
		return nil, Usage{}, err
	}

	return sub, usage, nil
}

// Usage returns what the owner uses of the plan limits.
func (uc *SubscriptionUseCase) Usage(ctx context.Context, userID string) (Usage, error) {
	garages, staff, err := uc.usage.OwnerUsage(ctx, userID)
	if err != nil {
		return Usage{}, toAppError("read usage", err)
	}

	transactions, err := uc.counter.Get(ctx, userID, uc.opts.today())
	if err != nil {
		return Usage{}, toAppError("read transactions", err)
	}

	return Usage{Garages: garages, Staff: staff, TransactionsToday: transactions}, nil
}

// persistChange saves sub, publishes the resulting events and audits it.
func (uc *SubscriptionUseCase) persistChange(ctx context.Context, sub *domain.Subscription, from domain.SubscriptionStatus, action string, before domain.Subscription) error {
	if err := uc.subscriptions.Update(ctx, sub); err != nil {
		return toAppError("save subscription", err)
	}

	if from != sub.Status {
		if err := uc.sync.statusChanged(ctx, sub, from, nil); err != nil {
			return toAppError("publish status change", err)
		}
	}

	if err := uc.sync.entitlementsChanged(ctx, sub); err != nil {
		return toAppError("sync entitlements", err)
	}

	err := uc.audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: action, EntityType: "subscription", EntityID: sub.ID,
		Before: subscriptionAudit(&before), After: subscriptionAudit(sub), IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit subscription", err)
	}

	return nil
}

func subscriptionAudit(s *domain.Subscription) map[string]any {
	return map[string]any{
		keyUserID: s.UserID, keyPlanID: s.PlanID, keyStatus: s.Status,
		"current_period_start": s.CurrentPeriodStart, "current_period_end": s.CurrentPeriodEnd,
	}
}
