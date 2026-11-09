package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// PlanFields are the settable plan fields; nil fields are left unchanged.
type PlanFields struct {
	Code                  *string
	Name                  *string
	Description           *string
	Price                 *int64
	Currency              *string
	BillingCycle          *domain.BillingCycle
	MaxGarages            *int
	MaxStaff              *int
	MaxTransactionsPerDay *int
	CountedKinds          *[]domain.TransactionKind
	TrialDays             *int
	Features              *[]domain.Feature
	Public                *bool
	Active                *bool
	SortOrder             *int
}

// ListAllPlans returns every plan (platform admins).
func (uc *SubscriptionUseCase) ListAllPlans(ctx context.Context) ([]*domain.Plan, error) {
	plans, err := uc.plans.ListAll(ctx)
	if err != nil {
		return nil, toAppError("list plans", err)
	}

	return plans, nil
}

// CreatePlan adds a plan to the catalogue (FR-LIC-05).
func (uc *SubscriptionUseCase) CreatePlan(ctx context.Context, fields PlanFields) (*domain.Plan, error) {
	plan := &domain.Plan{
		Currency: "VND", BillingCycle: domain.BillingCycleYearly, Active: true, Features: []domain.Feature{},
		CountedKinds: []domain.TransactionKind{domain.TransactionRepairOrder},
	}
	applyPlanFields(plan, fields)

	if err := plan.Validate(); err != nil {
		return nil, toAppError("create plan", err)
	}

	if err := uc.plans.Create(ctx, plan); err != nil {
		return nil, toAppError("create plan", err)
	}

	if err := uc.auditPlan(ctx, "plan.created", plan.ID, nil, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// UpdatePlan edits a plan (its code is immutable). Subscribers keep their
// plan; the new limits and features apply to them at once.
func (uc *SubscriptionUseCase) UpdatePlan(ctx context.Context, planID string, fields PlanFields) (*domain.Plan, error) {
	plan, err := uc.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, toAppError("update plan", err)
	}

	if fields.BillingCycle != nil && *fields.BillingCycle != plan.BillingCycle {
		subscribers, err := uc.plans.CountSubscribers(ctx, plan.ID)
		if err != nil {
			return nil, toAppError("update plan", err)
		}

		if subscribers > 0 {
			return nil, toAppError("update plan", domain.ErrPlanHasSubscribers)
		}
	}

	before := *plan
	fields.Code = nil
	applyPlanFields(plan, fields)

	if err := plan.Validate(); err != nil {
		return nil, toAppError("update plan", err)
	}

	if err := uc.plans.Update(ctx, plan); err != nil {
		return nil, toAppError("update plan", err)
	}

	if err := uc.auditPlan(ctx, "plan.updated", plan.ID, before, plan); err != nil {
		return nil, err
	}

	uc.invalidateSubscribers(ctx, plan.ID)

	return plan, nil
}

// invalidateSubscribers drops the cached entitlements of the plan's owners, so
// an edited limit, feature or counted kind applies at once and not when each
// cache entry expires. A failure only delays the change to the entry's TTL.
func (uc *SubscriptionUseCase) invalidateSubscribers(ctx context.Context, planID string) {
	owners, err := uc.plans.ListSubscriberIDs(ctx, planID)
	if err != nil {
		slog.Warn("license: listing the plan's subscribers failed", "plan_id", planID, "error", err)

		return
	}

	for _, owner := range owners {
		uc.sync.invalidate(ctx, owner)
	}
}

// ListSubscriptions lists subscriptions for platform admins.
func (uc *SubscriptionUseCase) ListSubscriptions(ctx context.Context, filter SubscriptionFilter) (*SubscriptionPage, error) {
	page, err := uc.subscriptions.Search(ctx, filter)
	if err != nil {
		return nil, toAppError("list subscriptions", err)
	}

	return page, nil
}

// ExtendSubscription renews a subscription manually by days (until online
// payment exists in Phase 2).
func (uc *SubscriptionUseCase) ExtendSubscription(ctx context.Context, subscriptionID string, days int) (*domain.Subscription, error) {
	sub, err := uc.subscriptions.LockByID(ctx, subscriptionID)
	if err != nil {
		return nil, toAppError("extend subscription", err)
	}

	before := *sub
	from := sub.Status

	if err := sub.Extend(days, uc.opts.now().UTC()); err != nil {
		return nil, toAppError("extend subscription", err)
	}

	if err := uc.persistChange(ctx, sub, from, "subscription.extended", before); err != nil {
		return nil, err
	}

	return sub, nil
}

func (uc *SubscriptionUseCase) auditPlan(ctx context.Context, action, planID string, before, after any) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: action, EntityType: "plan", EntityID: planID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit plan", err)
	}

	return nil
}

func applyPlanFields(p *domain.Plan, f PlanFields) {
	setString := func(dst, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}

	setInt := func(dst, src *int) {
		if src != nil {
			*dst = *src
		}
	}

	setString(&p.Code, f.Code)
	setString(&p.Name, f.Name)
	setString(&p.Description, f.Description)

	if f.Currency != nil {
		p.Currency = strings.ToUpper(strings.TrimSpace(*f.Currency))
	}

	if f.Price != nil {
		p.Price = *f.Price
	}

	if f.BillingCycle != nil {
		p.BillingCycle = *f.BillingCycle
	}

	setInt(&p.MaxGarages, f.MaxGarages)
	setInt(&p.MaxStaff, f.MaxStaff)
	setInt(&p.MaxTransactionsPerDay, f.MaxTransactionsPerDay)
	setInt(&p.SortOrder, f.SortOrder)
	setInt(&p.TrialDays, f.TrialDays)

	if f.CountedKinds != nil {
		p.CountedKinds = domain.NormalizeTransactionKinds(*f.CountedKinds)
	}

	if f.Features != nil {
		p.Features = domain.NormalizeFeatures(*f.Features)
	}

	if f.Public != nil {
		p.Public = *f.Public
	}

	if f.Active != nil {
		p.Active = *f.Active
	}
}
