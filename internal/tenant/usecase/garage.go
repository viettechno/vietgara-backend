package usecase

import (
	"context"

	"github.com/google/uuid"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// CreateGarageRequest is the input for GarageUseCase.Create.
type CreateGarageRequest struct {
	AccountID string
	Name      string
	TaxCode   *string
	Address   *string
	Timezone  *string
	Currency  *string
}

// UpdateGarageRequest is the input for GarageUseCase.Update; nil pointer
// fields keep their current value.
type UpdateGarageRequest struct {
	GarageID string
	Name     *string
	TaxCode  *string
	Address  *string
	LogoURL  *string
	Timezone *string
	Currency *string
}

// Option configures a GarageUseCase.
type Option func(*GarageUseCase)

// WithTransactionManager makes Create persist the garage, its owner staff
// record, and the garage.created outbox event inside a single Postgres
// transaction whose tenant context is the newly generated garage id -- the
// Row-Level Security policies require app.current_garage_id to be set to
// that id before the inserts can succeed.
func WithTransactionManager(txManager *platformdb.TxManager) Option {
	return func(uc *GarageUseCase) {
		uc.txManager = txManager
	}
}

// GarageUseCase (FR-TEN-01/02/03/05) implements the garage lifecycle
// actions that share the same repositories and can be injected as a single
// dependency.
type GarageUseCase struct {
	garageRepo     GarageRepository
	staffRepo      GarageStaffRepository
	eventPublisher EventPublisher
	txManager      *platformdb.TxManager
}

// NewGarageUseCase wires the garage use case to its repositories and the
// event publisher.
func NewGarageUseCase(garaRepo GarageRepository, staffRepo GarageStaffRepository, eventPublisher EventPublisher, opts ...Option) *GarageUseCase {
	uc := &GarageUseCase{garageRepo: garaRepo, staffRepo: staffRepo, eventPublisher: eventPublisher}

	for _, opt := range opts {
		opt(uc)
	}

	return uc
}

// Create validates the request, persists the garage together with an owner
// staff record, and publishes a garage.created event.
func (uc *GarageUseCase) Create(ctx context.Context, req CreateGarageRequest) (*domain.Garage, error) {
	if req.Name == "" {
		return nil, apperror.Wrap(apperror.CodeInvalidArgument, "garage name is required", domain.ErrGarageNameRequired)
	}

	gara := &domain.Garage{
		ID:        uuid.NewString(),
		AccountID: req.AccountID,
		Name:      req.Name,
		TaxCode:   req.TaxCode,
		Address:   req.Address,
		Timezone:  valueOrDefault(req.Timezone, "Asia/Ho_Chi_Minh"),
		Currency:  valueOrDefault(req.Currency, "VND"),
		Status:    domain.GarageStatusActive,
	}

	owner := &domain.GarageStaff{
		ID:        uuid.NewString(),
		GarageID:  gara.ID,
		AccountID: req.AccountID,
		Role:      domain.RoleOwner,
		Status:    domain.StaffStatusActive,
	}

	if uc.txManager != nil {
		// The garage id is only known after it is generated above, so the
		// tenant-scoped transaction is opened here (the interceptor layer
		// cannot know the id upfront) -- see middleware.TransactionInterceptor.
		if err := uc.txManager.WithTransaction(ctx, &gara.ID, func(txCtx context.Context) error {
			return uc.persist(txCtx, gara, owner)
		}); err != nil {
			return nil, apperror.Wrap(apperror.CodeInternal, "create garage", err)
		}

		return gara, nil
	}

	if err := uc.persist(ctx, gara, owner); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "create garage", err)
	}

	return gara, nil
}

// persist writes the garage, the owner staff record, and the outbox event
// using the querier carried in ctx (a transaction when one is active).
func (uc *GarageUseCase) persist(ctx context.Context, g *domain.Garage, owner *domain.GarageStaff) error {
	if err := uc.garageRepo.Create(ctx, g); err != nil {
		return err
	}

	if err := uc.staffRepo.Create(ctx, owner); err != nil {
		return err
	}

	if uc.eventPublisher != nil {
		return uc.eventPublisher.Publish(ctx, "garage.created", g.ID, map[string]string{
			"garage_id":  g.ID,
			"account_id": g.AccountID,
		})
	}

	return nil
}

// List returns every garage owned by accountID.
func (uc *GarageUseCase) List(ctx context.Context, accountID string) ([]*domain.Garage, error) {
	garages, err := uc.garageRepo.ListByAccountID(ctx, accountID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "list garages", err)
	}

	return garages, nil
}

// Get returns the garage with the given id.
func (uc *GarageUseCase) Get(ctx context.Context, garageID string) (*domain.Garage, error) {
	g, err := uc.garageRepo.FindByID(ctx, garageID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeNotFound, "garage not found", err)
	}

	return g, nil
}

// Update applies the partial updates in req to the matching garage and
// persists it.
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

	if req.Timezone != nil {
		g.Timezone = *req.Timezone
	}

	if req.Currency != nil {
		g.Currency = *req.Currency
	}

	if err := uc.garageRepo.Update(ctx, g); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "update garage", err)
	}

	return g, nil
}

// valueOrDefault returns def when v is nil, otherwise *v.
func valueOrDefault(v *string, def string) string {
	if v != nil {
		return *v
	}

	return def
}
