package usecase

import (
	"context"
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// EntitlementUseCase answers "what may this owner's garages use right now?"
// from the entitlement cache, falling back to the subscription on a miss
// (LLD 3.3). It implements the LLD's EntitlementChecker, the license
// enforcement of the request middleware and the Tenant module's limits.
type EntitlementUseCase struct {
	subscriptions SubscriptionRepository
	cache         EntitlementCache
	owners        GarageOwnerReader
	counter       TransactionCounter
	trials        *SubscriptionUseCase
	opts          options
}

// NewEntitlementUseCase wires the entitlement use case.
func NewEntitlementUseCase(
	subscriptions SubscriptionRepository, cache EntitlementCache, owners GarageOwnerReader,
	counter TransactionCounter, trials *SubscriptionUseCase, opts ...Option,
) *EntitlementUseCase {
	return &EntitlementUseCase{
		subscriptions: subscriptions, cache: cache, owners: owners, counter: counter, trials: trials, opts: newOptions(opts),
	}
}

// Get returns the owner's entitlements. Cache failures are logged and
// served from the database so licensing never takes the API down.
func (uc *EntitlementUseCase) Get(ctx context.Context, userID string) (*domain.Entitlements, error) {
	cached, err := uc.cache.Get(ctx, userID)
	if err != nil {
		slog.Warn("license: reading the entitlement cache failed", "user_id", userID, "error", err)
	}

	// An entry cached before CountedKinds existed lacks it (nil; a plan that
	// counts nothing caches an empty list): read the database instead.
	if cached != nil && cached.CountedKinds != nil {
		return cached, nil
	}

	sub, err := uc.subscriptions.FindByUserID(ctx, userID)
	if err != nil {
		return nil, toAppError("load entitlements", err)
	}

	e := domain.NewEntitlements(sub)
	if err := uc.cache.Set(ctx, e); err != nil {
		slog.Warn("license: writing the entitlement cache failed", "user_id", userID, "error", err)
	}

	return e, nil
}

// ForGarage returns the entitlements of the garage's owner, starting the
// owner's trial if they have no subscription yet.
func (uc *EntitlementUseCase) ForGarage(ctx context.Context, garageID string) (*domain.Entitlements, error) {
	owner, err := uc.garageOwner(ctx, garageID)
	if err != nil {
		return nil, err
	}

	return uc.getOrStartTrial(ctx, owner)
}

func (uc *EntitlementUseCase) getOrStartTrial(ctx context.Context, userID string) (*domain.Entitlements, error) {
	e, err := uc.Get(ctx, userID)
	if err == nil || !errors.Is(err, domain.ErrSubscriptionNotFound) {
		return e, err
	}

	if err := uc.trials.EnsureTrial(ctx, userID); err != nil {
		return nil, err
	}

	return uc.Get(ctx, userID)
}

// Invalidate drops the owner's cache entry.
func (uc *EntitlementUseCase) Invalidate(ctx context.Context, userID string) error {
	return uc.cache.Delete(ctx, userID)
}

// IsFeatureEnabled reports whether the owner's garages may use feature.
func (uc *EntitlementUseCase) IsFeatureEnabled(ctx context.Context, userID string, feature domain.Feature) (bool, error) {
	e, err := uc.Get(ctx, userID)
	if err != nil {
		return false, err
	}

	return e.HasFeature(feature), nil
}

// LicenseMode implements middleware.LicenseChecker from the garage owner's
// subscription. An owner without a subscription yet is not blocked.
func (uc *EntitlementUseCase) LicenseMode(ctx context.Context, garageID string) (middleware.LicenseMode, error) {
	owner, err := uc.garageOwner(ctx, garageID)
	if err != nil {
		return middleware.LicenseFull, err
	}

	e, err := uc.Get(ctx, owner)
	if err != nil {
		if errors.Is(err, domain.ErrSubscriptionNotFound) {
			return middleware.LicenseFull, nil
		}

		return middleware.LicenseFull, err
	}

	switch {
	case !e.Status.AllowsReads():
		return middleware.LicenseLocked, nil
	case !e.Status.AllowsWrites():
		return middleware.LicenseReadOnly, nil
	default:
		return middleware.LicenseFull, nil
	}
}

// CheckLimit refuses a new garage or staff member once current usage
// reached the owner's plan limit (the Tenant module's LimitChecker, through
// an adapter in cmd/server).
func (uc *EntitlementUseCase) CheckLimit(ctx context.Context, ownerID string, limitType domain.LimitType, current int) error {
	e, err := uc.getOrStartTrial(ctx, ownerID)
	if err != nil {
		return err
	}

	within, err := e.WithinLimit(limitType, current)
	if err != nil {
		return toAppError("check limit", err)
	}

	if !within {
		allowed, _ := e.Limit(limitType)

		return limitReached(limitType, allowed)
	}

	return nil
}

// ConsumeTransaction counts one business document of kind (quote,
// repair_order or settlement) created by the owner's garages today and
// refuses it once the plan's daily limit is reached (a refused document is
// never counted). A kind the plan does not count is neither counted nor
// limited (FR-LIC-02). Called by the document modules, which call
// ReleaseTransaction when the document is not created after all.
func (uc *EntitlementUseCase) ConsumeTransaction(ctx context.Context, ownerID, kind string) error {
	e, err := uc.getOrStartTrial(ctx, ownerID)
	if err != nil {
		return err
	}

	if !e.CountsTransaction(domain.TransactionKind(kind)) {
		return nil
	}

	counted, err := uc.counter.TryIncrement(ctx, ownerID, uc.opts.today(), e.MaxTransactionsPerDay)
	if err != nil {
		return toAppError("count transaction", err)
	}

	if !counted {
		return limitReached(domain.LimitTransactions, e.MaxTransactionsPerDay)
	}

	return nil
}

// ReleaseTransaction takes back the count of ConsumeTransaction when the
// document of kind was not created (a later step failed or the request was
// rolled back), so a failure does not use up the owner's day. It is best
// effort: a failure is logged and never hides the error that caused it.
func (uc *EntitlementUseCase) ReleaseTransaction(ctx context.Context, ownerID, kind string) {
	// The request may be cancelled by now; the count must still be given back.
	ctx = context.WithoutCancel(ctx)

	e, err := uc.Get(ctx, ownerID)
	if err != nil {
		slog.Warn("license: releasing a transaction failed", "user_id", ownerID, "kind", kind, "error", err)

		return
	}

	if e.CountsTransaction(domain.TransactionKind(kind)) {
		uc.giveBack(ctx, ownerID)
	}
}

func (uc *EntitlementUseCase) giveBack(ctx context.Context, ownerID string) {
	if err := uc.counter.Decrement(ctx, ownerID, uc.opts.today()); err != nil {
		slog.Warn("license: giving a transaction back failed", "user_id", ownerID, "error", err)
	}
}

// garageOwner reads the owner from the request's authorized access, or
// asks the Tenant module.
func (uc *EntitlementUseCase) garageOwner(ctx context.Context, garageID string) (string, error) {
	if user, ok := access.FromContext(ctx); ok && user.GarageID == garageID && user.OwnerUserID != "" {
		return user.OwnerUserID, nil
	}

	owner, err := uc.owners.GarageOwner(ctx, garageID)
	if err != nil {
		return "", toAppError("find garage owner", err)
	}

	return owner, nil
}
