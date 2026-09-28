package main

import (
	"context"
	"errors"

	identityDomain "github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	licenseDomain "github.com/viettechno/vietgara-backend/internal/license/domain"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// The adapters below connect modules through narrow interfaces owned by the
// consuming module, so no module imports another (ADR-001: modules stay
// splittable into services).

// accountDirectory gives the Tenant module a read-only view of Identity
// accounts.
type accountDirectory struct {
	accounts *identityRepo.PostgresAccountRepository
}

func (a accountDirectory) FindByEmail(ctx context.Context, email string) (*tenantUsecase.AccountSummary, error) {
	normalized, ok := identityDomain.NormalizeEmail(email)
	if !ok {
		return nil, tenantDomain.ErrAccountNotFound
	}

	return summaryOrNotFound(a.accounts.FindByEmail(ctx, normalized))
}

func (a accountDirectory) FindByPhone(ctx context.Context, phone string) (*tenantUsecase.AccountSummary, error) {
	normalized, ok := identityDomain.NormalizePhone(phone)
	if !ok {
		return nil, tenantDomain.ErrAccountNotFound
	}

	return summaryOrNotFound(a.accounts.FindByPhone(ctx, normalized))
}

func (a accountDirectory) FindByID(ctx context.Context, id string) (*tenantUsecase.AccountSummary, error) {
	return summaryOrNotFound(a.accounts.FindByID(ctx, id))
}

func (a accountDirectory) ListByIDs(ctx context.Context, ids []string) (map[string]*tenantUsecase.AccountSummary, error) {
	accounts, err := a.accounts.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*tenantUsecase.AccountSummary, len(accounts))
	for _, acc := range accounts {
		result[acc.ID] = toSummary(acc)
	}

	return result, nil
}

func summaryOrNotFound(acc *identityDomain.Account, err error) (*tenantUsecase.AccountSummary, error) {
	if err != nil {
		if errors.Is(err, identityDomain.ErrAccountNotFound) {
			return nil, tenantDomain.ErrAccountNotFound
		}

		return nil, err
	}

	return toSummary(acc), nil
}

func toSummary(acc *identityDomain.Account) *tenantUsecase.AccountSummary {
	return &tenantUsecase.AccountSummary{
		ID:            acc.ID,
		FullName:      acc.FullName,
		Email:         acc.Email,
		Phone:         acc.Phone,
		Locale:        string(acc.Locale.OrDefault()),
		Active:        acc.Status == identityDomain.AccountStatusActive,
		EmailVerified: acc.EmailVerified(),
	}
}

// limitChecker lets the Tenant module enforce License plan limits.
type limitChecker struct {
	entitlements *licenseUsecase.EntitlementUseCase
}

func (l limitChecker) CheckLimit(ctx context.Context, ownerID string, limit tenantUsecase.Limit, current int) error {
	return l.entitlements.CheckLimit(ctx, ownerID, licenseDomain.LimitType(limit), current)
}

// garageOwners lets the License module find a garage's owner.
type garageOwners struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g garageOwners) GarageOwner(ctx context.Context, garageID string) (string, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return "", err
	}

	return garage.OwnerAccountID, nil
}
