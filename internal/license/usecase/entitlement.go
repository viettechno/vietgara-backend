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
func (uc *EntitlementUseCase) Get(ctx context.Context, accountID string) (*domain.Entitlements, error) {
	cached, err := uc.cache.Get(ctx, accountID)
	if err != nil {
		slog.Warn("license: reading the entitlement cache failed", "account_id", accountID, "error", err)
	}

	if cached != nil {
		return cached, nil
	}

	sub, err := uc.subscriptions.FindByAccountID(ctx, accountID)
	if err != nil {
		return nil, toAppError("load entitlements", err)
	}

	e := domain.NewEntitlements(sub)
	if err := uc.cache.Set(ctx, e); err != nil {
		slog.Warn("license: writing the entitlement cache failed", "account_id", accountID, "error", err)
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

func (uc *EntitlementUseCase) getOrStartTrial(ctx context.Context, accountID string) (*domain.Entitlements, error) {
	e, err := uc.Get(ctx, accountID)
	if err == nil || !errors.Is(err, domain.ErrSubscriptionNotFound) {
		return e, err
	}

	if err := uc.trials.EnsureTrial(ctx, accountID); err != nil {
		return nil, err
	}

	return uc.Get(ctx, accountID)
}

// Invalidate drops the owner's cache entry.
func (uc *EntitlementUseCase) Invalidate(ctx context.Context, accountID string) error {
	return uc.cache.Delete(ctx, accountID)
}

// IsFeatureEnabled reports whether the owner's garages may use feature.
func (uc *EntitlementUseCase) IsFeatureEnabled(ctx context.Context, accountID string, feature domain.Feature) (bool, error) {
	e, err := uc.Get(ctx, accountID)
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

// ConsumeTransaction counts one business document (quote, repair order,
// settlement) created by the owner's garages today and refuses it once the
// plan's daily limit is reached. Called by the document modules from
// Release 1.2.
func (uc *EntitlementUseCase) ConsumeTransaction(ctx context.Context, ownerID string) error {
	e, err := uc.getOrStartTrial(ctx, ownerID)
	if err != nil {
		return err
	}

	count, err := uc.counter.Increment(ctx, ownerID, uc.opts.today())
	if err != nil {
		return toAppError("count transaction", err)
	}

	if count > e.MaxTransactionsPerDay {
		return limitReached(domain.LimitTransactions, e.MaxTransactionsPerDay)
	}

	return nil
}

// garageOwner reads the owner from the request's authorized access, or
// asks the Tenant module.
func (uc *EntitlementUseCase) garageOwner(ctx context.Context, garageID string) (string, error) {
	if acc, ok := access.FromContext(ctx); ok && acc.GarageID == garageID && acc.OwnerAccountID != "" {
		return acc.OwnerAccountID, nil
	}

	owner, err := uc.owners.GarageOwner(ctx, garageID)
	if err != nil {
		return "", toAppError("find garage owner", err)
	}

	return owner, nil
}
