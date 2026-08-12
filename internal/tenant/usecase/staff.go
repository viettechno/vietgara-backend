package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// AddStaffRequest is the input for StaffUseCase.Add.
type AddStaffRequest struct {
	GarageID  string
	AccountID string
	Role      domain.Role
}

// StaffUseCase (FR-TEN-04) implements the staff membership operations that
// share the same repository and are used together in garage management
// flows.
type StaffUseCase struct {
	staff repository.GarageStaffRepository
}

// NewStaffUseCase wires the staff use case to its repository.
func NewStaffUseCase(staff repository.GarageStaffRepository) *StaffUseCase {
	return &StaffUseCase{staff: staff}
}

// Add validates the role, rejects duplicate active memberships, and
// persists the new staff record.
func (uc *StaffUseCase) Add(ctx context.Context, req AddStaffRequest) (*domain.GarageStaff, error) {
	if !req.Role.IsValid() {
		return nil, apperror.Wrap(apperror.CodeInvalidArgument, "invalid role", domain.ErrInvalidRole)
	}

	existing, err := uc.staff.FindByGarageAndAccount(ctx, req.GarageID, req.AccountID)
	if err == nil && existing != nil && existing.Status == domain.StaffStatusActive {
		return nil, apperror.Wrap(apperror.CodeAlreadyExists, "account is already staff at this garage", domain.ErrStaffAlreadyExists)
	}

	s := &domain.GarageStaff{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		AccountID: req.AccountID,
		Role:      req.Role,
		Status:    domain.StaffStatusActive,
	}
	if err := uc.staff.Create(ctx, s); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "add staff", err)
	}

	return s, nil
}

// List returns the active staff members of garageID.
func (uc *StaffUseCase) List(ctx context.Context, garageID string) ([]*domain.GarageStaff, error) {
	list, err := uc.staff.ListByGarageID(ctx, garageID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "list staff", err)
	}

	return list, nil
}
