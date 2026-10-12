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

// ChangeMyPlan chooses another plan (FR-LIC-02). An upgrade and a downgrade
// both take effect when the current period ends (when the trial ends,
// during the trial), so the plan is scheduled and the answer carries it as
// the pending change; choosing again replaces it, and choosing the plan the
// owner already has cancels it. A past-due or suspended owner has no period
// left to wait for: the plan, the current one included, takes effect at
// once, which is how they recover. A plan whose limits the current usage
// already exceeds is refused; the limits are checked again on the
// effective date.
func (uc *SubscriptionUseCase) ChangeMyPlan(ctx context.Context, userID, planID string) (*domain.Subscription, Usage, error) {
	if _, _, err := uc.GetMine(ctx, userID); err != nil {
		return nil, Usage{}, err
	}

	sub, err := uc.subscriptions.LockByUserID(ctx, userID)
	if err != nil {
		return nil, Usage{}, toAppError("change plan", err)
	}

	plan, err := uc.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, Usage{}, toAppError("change plan", err)
	}

	if !plan.Selectable() {
		return nil, Usage{}, toAppError("change plan", domain.ErrPlanNotAvailable)
	}

	usage, err := uc.Usage(ctx, userID)
	if err != nil {
		return nil, Usage{}, err
	}

	if err := checkLimits(usage, plan); err != nil {
		return nil, Usage{}, err
	}

	if sub.Status == domain.StatusCancelled {
		return nil, Usage{}, toAppError("change plan", domain.ErrSubscriptionCancelled)
	}

	if sub.HoldsPeriod() {
		sub, err = uc.schedule(ctx, sub, plan)
		if err != nil {
			return nil, Usage{}, err
		}

		return sub, usage, nil
	}

	before := *sub
	from := sub.Status

	if err := uc.cancelPending(ctx, sub); err != nil {
		return nil, Usage{}, err
	}

	if err := sub.ChangePlan(plan, uc.opts.now().UTC()); err != nil {
		return nil, Usage{}, toAppError("change plan", err)
	}

	if err := uc.persistChange(ctx, sub, from, "subscription.plan_changed", before); err != nil {
		return nil, Usage{}, err
	}

	return sub, usage, nil
}

// checkLimits refuses a plan whose limits the current usage already exceeds.
func checkLimits(usage Usage, plan *domain.Plan) error {
	switch {
	case usage.Garages > plan.MaxGarages:
		return limitReached(domain.LimitGarages, plan.MaxGarages)
	case usage.Staff > plan.MaxStaff:
		return limitReached(domain.LimitStaff, plan.MaxStaff)
	default:
		return nil
	}
}

// schedule makes plan the pending change of a subscription that holds its
// period: it replaces an earlier pending change, and the current plan
// cancels it.
func (uc *SubscriptionUseCase) schedule(ctx context.Context, sub *domain.Subscription, plan *domain.Plan) (*domain.Subscription, error) {
	before := sub.PendingChange

	if err := uc.cancelPending(ctx, sub); err != nil {
		return nil, err
	}

	if plan.ID == sub.PlanID {
		return sub, uc.auditChange(ctx, sub, "subscription.plan_change_cancelled", before)
	}

	now := uc.opts.now().UTC()
	change := &domain.PlanChange{
		SubscriptionID: sub.ID, PlanID: plan.ID, Plan: plan, Status: domain.PlanChangeScheduled,
		EffectiveAt: sub.CurrentPeriodEnd, RequestedAt: now,
	}

	if err := uc.subscriptions.CreateChange(ctx, change); err != nil {
		return nil, toAppError("schedule plan change", err)
	}

	sub.PendingChange = change

	return sub, uc.auditChange(ctx, sub, "subscription.plan_change_scheduled", before)
}

// cancelPending cancels the subscription's pending change, if any.
func (uc *SubscriptionUseCase) cancelPending(ctx context.Context, sub *domain.Subscription) error {
	change := sub.PendingChange
	if change == nil {
		return nil
	}

	now := uc.opts.now().UTC()
	change.Status, change.CancelledAt = domain.PlanChangeCancelled, &now

	if err := uc.subscriptions.SaveChange(ctx, change); err != nil {
		return toAppError("cancel plan change", err)
	}

	sub.PendingChange = nil

	return nil
}

// CancelMyPendingPlanChange cancels the owner's pending plan change
// (FR-LIC-02); ErrNoPendingPlanChange (not found) when there is none.
func (uc *SubscriptionUseCase) CancelMyPendingPlanChange(ctx context.Context, userID string) (*domain.Subscription, Usage, error) {
	sub, err := uc.subscriptions.LockByUserID(ctx, userID)
	if err != nil {
		return nil, Usage{}, toAppError("cancel plan change", err)
	}

	before := sub.PendingChange
	if before == nil {
		return nil, Usage{}, toAppError("cancel plan change", domain.ErrNoPendingPlanChange)
	}

	if err := uc.cancelPending(ctx, sub); err != nil {
		return nil, Usage{}, err
	}

	if err := uc.auditChange(ctx, sub, "subscription.plan_change_cancelled", before); err != nil {
		return nil, Usage{}, err
	}

	usage, err := uc.Usage(ctx, userID)
	if err != nil {
		return nil, Usage{}, err
	}

	return sub, usage, nil
}

// auditChange records a plan change being scheduled or cancelled: the
// pending plan before and after.
func (uc *SubscriptionUseCase) auditChange(ctx context.Context, sub *domain.Subscription, action string, before *domain.PlanChange) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: action, EntityType: "subscription", EntityID: sub.ID,
		Before: pendingAudit(before), After: pendingAudit(sub.PendingChange), IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit subscription", err)
	}

	return nil
}

func pendingAudit(c *domain.PlanChange) map[string]any {
	if c == nil {
		return map[string]any{}
	}

	return map[string]any{"plan_id": c.PlanID, "status": c.Status, "effective_at": c.EffectiveAt}
}

// persistChange saves sub, publishes the resulting events and audits it.
func (uc *SubscriptionUseCase) persistChange(ctx context.Context, sub *domain.Subscription, from domain.SubscriptionStatus, action string, before domain.Subscription) error {
	if err := uc.subscriptions.Update(ctx, sub); err != nil {
		return toAppError("save subscription", err)
	}

	if from != sub.Status {
		if err := uc.sync.statusChanged(ctx, sub, from); err != nil {
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
		keyUserID: s.UserID, "plan_id": s.PlanID, "status": s.Status,
		"current_period_start": s.CurrentPeriodStart, "current_period_end": s.CurrentPeriodEnd,
	}
}
