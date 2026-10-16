package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// UnitFields are the settable fields of a unit; nil fields are left
// unchanged (on create: defaulted).
type UnitFields struct {
	Name         *string
	Abbreviation *string
	IsActive     *bool
}

// CreateUnitRequest is the input for UseCase.CreateUnit.
type CreateUnitRequest struct {
	GarageID string
	ActorID  string
	Fields   UnitFields
}

// UpdateUnitRequest is the input for UseCase.UpdateUnit.
type UpdateUnitRequest struct {
	GarageID string
	UnitID   string
	ActorID  string
	Fields   UnitFields
}

// ListUnitsRequest is the input for UseCase.ListUnits.
type ListUnitsRequest struct {
	PageRequest

	GarageID        string
	IncludeInactive bool
}

// DeleteRequest is the input of the delete-or-deactivate calls.
type DeleteRequest struct {
	GarageID string
	ID       string
	ActorID  string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// UseCase is the Catalogue module's application service.
type UseCase struct {
	repo  Repository
	usage UsageChecker
	files Files
}

// NewUseCase wires the use case to its repository and to the ports that
// reach the Inventory/Quote/Repair Order usage and the File module.
func NewUseCase(repo Repository, usage UsageChecker, files Files) *UseCase {
	return &UseCase{repo: repo, usage: usage, files: files}
}

// CreateUnit adds a unit of measure to the garage (FR-CAT-01).
func (uc *UseCase) CreateUnit(ctx context.Context, req CreateUnitRequest) (*domain.Unit, error) {
	unit := &domain.Unit{
		ID: uuid.NewString(), GarageID: req.GarageID, IsActive: true,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if req.Fields.Name == nil {
		return nil, toAppError("create unit", domain.ErrUnitNameRequired)
	}

	if req.Fields.Abbreviation == nil {
		return nil, toAppError("create unit", domain.ErrUnitAbbrevRequired)
	}

	if err := applyUnitFields(unit, req.Fields); err != nil {
		return nil, toAppError("create unit", err)
	}

	if err := uc.repo.CreateUnit(ctx, unit); err != nil {
		return nil, toAppError("create unit", err)
	}

	return unit, nil
}

// UpdateUnit applies the partial updates in req to a unit. Deactivating a
// unit is allowed whatever uses it; it only leaves the pickers.
func (uc *UseCase) UpdateUnit(ctx context.Context, req UpdateUnitRequest) (*domain.Unit, error) {
	unit, err := uc.repo.FindUnit(ctx, req.GarageID, req.UnitID)
	if err != nil {
		return nil, toAppError("update unit", err)
	}

	if err := applyUnitFields(unit, req.Fields); err != nil {
		return nil, toAppError("update unit", err)
	}

	unit.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.UpdateUnit(ctx, unit); err != nil {
		return nil, toAppError("update unit", err)
	}

	return unit, nil
}

// ListUnits returns one page of the garage's units, by name.
func (uc *UseCase) ListUnits(ctx context.Context, req ListUnitsRequest) (*Page[*domain.Unit], error) {
	page := req.normalize()

	units, total, err := uc.repo.ListUnits(ctx, req.GarageID, UnitFilter{
		IncludeInactive: req.IncludeInactive, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list units", err)
	}

	return &Page[*domain.Unit]{Items: units, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// DeleteUnit follows the delete-or-deactivate rule (FR-CAT-07): a unit that a
// part, a stock movement or a document line uses is deactivated, any other
// is deleted.
func (uc *UseCase) DeleteUnit(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	if _, err := uc.repo.FindUnit(ctx, req.GarageID, req.ID); err != nil {
		return "", toAppError("delete unit", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			return lifecycle.AnyInUse(ctx,
				func(ctx context.Context) (bool, error) { return uc.repo.UnitUsedByParts(ctx, req.GarageID, req.ID) },
				func(ctx context.Context) (bool, error) { return uc.usage.UnitInUse(ctx, req.GarageID, req.ID) },
			)
		},
		Delete: func(ctx context.Context) error {
			return uc.repo.SoftDeleteUnit(ctx, req.GarageID, req.ID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.repo.SetUnitActive(ctx, req.GarageID, req.ID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete unit", err)
	}

	return outcome, nil
}

func applyUnitFields(unit *domain.Unit, f UnitFields) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if name == "" {
			return domain.ErrUnitNameRequired
		}

		if err := domain.CheckLength(name, domain.MaxUnitNameLen); err != nil {
			return err
		}

		unit.Name = name
	}

	if f.Abbreviation != nil {
		abbrev := strings.TrimSpace(*f.Abbreviation)
		if abbrev == "" {
			return domain.ErrUnitAbbrevRequired
		}

		if err := domain.CheckLength(abbrev, domain.MaxUnitAbbrevLen); err != nil {
			return err
		}

		unit.Abbreviation = abbrev
	}

	if f.IsActive != nil {
		unit.IsActive = *f.IsActive
	}

	return nil
}
