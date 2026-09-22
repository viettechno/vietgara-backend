package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// AddStaffRequest is the input for StaffUseCase.Add.
type AddStaffRequest struct {
	GarageID  string
	AccountID string
	Role      domain.Role
}

// UpdateStaffRequest is the input for StaffUseCase.Update; nil pointer
// fields keep their current value.
type UpdateStaffRequest struct {
	StaffID  string
	GarageID string
	Role     *domain.Role
	Status   *domain.StaffStatus
}

// StaffUseCase (FR-TEN-04) implements the staff membership operations that
// share the same repository and are used together in garage management
// flows.
type StaffUseCase struct {
	staff   GarageStaffRepository
	account AccountDirectory
}

// NewStaffUseCase wires the staff use case to its repository.
func NewStaffUseCase(staff GarageStaffRepository, opts ...StaffOption) *StaffUseCase {
	uc := &StaffUseCase{staff: staff}

	for _, opt := range opts {
		opt(uc)
	}

	return uc
}

// Add validates the role, rejects duplicate active memberships, and
// persists the new staff record.
func (uc *StaffUseCase) Add(ctx context.Context, req AddStaffRequest) (*domain.GarageStaff, error) {
	if !req.Role.IsValid() {
		return nil, apperror.Wrap(apperror.CodeInvalidArgument, "invalid role", domain.ErrInvalidRole)
	}

	if err := uc.requireActiveAccount(ctx, req.AccountID); err != nil {
		return nil, err
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

// Update applies the partial updates in req to a staff membership. The
// garage ID must match the membership's own garage so IDs cannot be mixed
// across tenants. Deactivating the garage owner is rejected: the owner is
// whoever created the garage and must always retain access.
func (uc *StaffUseCase) Update(ctx context.Context, req UpdateStaffRequest) (*domain.GarageStaff, error) {
	s, err := uc.staff.FindByID(ctx, req.StaffID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.Wrap(apperror.CodeNotFound, "staff membership not found", domain.ErrStaffNotFound)
		}

		return nil, apperror.Wrap(apperror.CodeInternal, "find staff", err)
	}

	if s.GarageID != req.GarageID {
		return nil, apperror.Wrap(apperror.CodeNotFound, "staff membership not found", domain.ErrStaffNotFound)
	}

	if req.Role != nil {
		if !req.Role.IsValid() {
			return nil, apperror.Wrap(apperror.CodeInvalidArgument, "invalid role", domain.ErrInvalidRole)
		}

		if *req.Role == domain.RoleOwner {
			return nil, apperror.Wrap(apperror.CodeInvalidArgument, "owners cannot be invited", domain.ErrInvalidRole)
		}

		s.Role = *req.Role
	}

	if req.Status != nil {
		if !req.Status.IsValid() {
			return nil, apperror.Wrap(apperror.CodeInvalidArgument, "invalid staff status", domain.ErrInvalidStaffStatus)
		}

		if *req.Status != domain.StaffStatusActive && s.Role == domain.RoleOwner {
			return nil, apperror.Wrap(apperror.CodeFailedPrecondition, "the garage owner cannot be deactivated", domain.ErrOwnerDeactivation)
		}

		s.Status = *req.Status
	}

	if err := uc.staff.Update(ctx, s); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "update staff", err)
	}

	return s, nil
}

// Deactivate marks a staff membership inactive. It is idempotent:
// deactivating an already-inactive membership succeeds without changes.
func (uc *StaffUseCase) Deactivate(ctx context.Context, garageID, staffID string) (*domain.GarageStaff, error) {
	inactive := domain.StaffStatusInactive

	return uc.Update(ctx, UpdateStaffRequest{StaffID: staffID, GarageID: garageID, Status: &inactive})
}
