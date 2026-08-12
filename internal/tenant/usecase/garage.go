package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// GarageUseCase (FR-TEN-01/02/03/05): combines the garage lifecycle actions
// that share the same repositories and can be injected as a single dependency.
// ---------------------------------------------------------------------------

type CreateGarageRequest struct {
	AccountID string
	Name      string
	TaxCode   *string
	Address   *string
}

type UpdateGarageRequest struct {
	GarageID string
	Name     *string
	TaxCode  *string
	Address  *string
	LogoURL  *string
}

type GarageUseCase struct {
	garageRepo     repository.GarageRepository
	staffRepo      repository.GarageStaffRepository
	eventPublisher EventPublisher
}

func NewGarageUseCase(garage repository.GarageRepository, staff repository.GarageStaffRepository, eventPublisher EventPublisher) *GarageUseCase {
	return &GarageUseCase{garageRepo: garage, staffRepo: staff, eventPublisher: eventPublisher}
}

func (uc *GarageUseCase) Create(ctx context.Context, req CreateGarageRequest) (*domain.Garage, error) {
	if req.Name == "" {
		return nil, apperror.Wrap(apperror.CodeInvalidArgument, "garage name is required", domain.ErrGarageNameRequired)
	}

	g := &domain.Garage{
		ID:        uuid.NewString(),
		AccountID: req.AccountID,
		Name:      req.Name,
		TaxCode:   req.TaxCode,
		Address:   req.Address,
		Timezone:  "Asia/Ho_Chi_Minh",
		Currency:  "VND",
		Status:    domain.GarageStatusActive,
	}
	if err := uc.garageRepo.Create(ctx, g); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "create garage", err)
	}

	owner := &domain.GarageStaff{
		ID:        uuid.NewString(),
		GarageID:  g.ID,
		AccountID: req.AccountID,
		Role:      domain.RoleOwner,
		Status:    domain.StaffStatusActive,
	}
	if err := uc.staffRepo.Create(ctx, owner); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "create owner staff record", err)
	}

	if uc.eventPublisher != nil {
		if err := uc.eventPublisher.Publish(ctx, "garage.created", g.ID, map[string]string{
			"garage_id":  g.ID,
			"account_id": g.AccountID,
		}); err != nil {
			return nil, apperror.Wrap(apperror.CodeInternal, "publish garage.created event", err)
		}
	}

	return g, nil
}

func (uc *GarageUseCase) List(ctx context.Context, accountID string) ([]*domain.Garage, error) {
	garages, err := uc.garageRepo.ListByAccountID(ctx, accountID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "list garages", err)
	}

	return garages, nil
}

func (uc *GarageUseCase) Update(ctx context.Context, req UpdateGarageRequest) (*domain.Garage, error) {
	g, err := uc.garageRepo.FindByID(ctx, req.GarageID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeNotFound, "garage not found", err)
	}

	if req.Name != nil {
		g.Name = *req.Name
	}

	if req.TaxCode != nil {
		g.TaxCode = req.TaxCode
	}

	if req.Address != nil {
		g.Address = req.Address
	}

	if req.LogoURL != nil {
		g.LogoURL = req.LogoURL
	}

	if err := uc.garageRepo.Update(ctx, g); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "update garage", err)
	}

	return g, nil
}
