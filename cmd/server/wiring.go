package main

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// tenantToIdentityGarageReaderAdapter bridges the Tenant module's
// GarageStaffRepository to the narrower GarageMembershipReader interface
// the Identity module depends on -- avoids a circular import between the
// two modules while both still live in the same Modular Monolith binary
// (ADR-001). Converts tenant repository.RoleAssignment into
// identity usecase.GarageRole.
type tenantToIdentityGarageReaderAdapter struct {
	staffRepo *tenantRepo.PostgresGarageStaffRepository
}

func (a *tenantToIdentityGarageReaderAdapter) ListGarageRolesForAccount(ctx context.Context, accountID string) ([]identityUsecase.GarageRole, error) {
	assignments, err := a.staffRepo.ListRolesForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := make([]identityUsecase.GarageRole, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, identityUsecase.GarageRole{GarageID: assignment.GarageID, Role: assignment.Role})
	}

	return result, nil
}

// identityToTenantAccountDirectoryAdapter gives the Tenant module a narrow
// read-only view of Identity accounts (email/phone lookup and id batches)
// without importing the Identity module -- the mirror image of
// tenantToIdentityGarageReaderAdapter.
type identityToTenantAccountDirectoryAdapter struct {
	accountRepo *identityRepo.PostgresAccountRepository
}

func (a *identityToTenantAccountDirectoryAdapter) FindByEmail(ctx context.Context, email string) (*tenantUsecase.AccountSummary, error) {
	return summaryOrNotFound(a.accountRepo.FindByEmailFold(ctx, email))
}

func (a *identityToTenantAccountDirectoryAdapter) FindByPhone(ctx context.Context, phone string) (*tenantUsecase.AccountSummary, error) {
	return summaryOrNotFound(a.accountRepo.FindByPhoneNormalized(ctx, phone))
}

func (a *identityToTenantAccountDirectoryAdapter) ListByIDs(ctx context.Context, ids []string) (map[string]*tenantUsecase.AccountSummary, error) {
	accounts, err := a.accountRepo.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*tenantUsecase.AccountSummary, len(accounts))
	for _, acc := range accounts {
		result[acc.ID] = toAccountSummary(acc)
	}

	return result, nil
}

func summaryOrNotFound(acc *domain.Account, err error) (*tenantUsecase.AccountSummary, error) {
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, tenantDomain.ErrAccountNotFound
		}

		return nil, err
	}

	return toAccountSummary(acc), nil
}

func toAccountSummary(acc *domain.Account) *tenantUsecase.AccountSummary {
	return &tenantUsecase.AccountSummary{
		ID:       acc.ID,
		FullName: acc.FullName,
		Email:    acc.Email,
		Phone:    acc.Phone,
		Active:   acc.Status == domain.AccountStatusActive,
	}
}
