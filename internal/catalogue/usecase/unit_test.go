package usecase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

func TestUseCase_CreateUnit(t *testing.T) {
	t.Parallel()

	t.Run("creates an active unit with trimmed fields", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateUnit(mock.Anything, mock.MatchedBy(func(u *domain.Unit) bool {
			return u.Name == "Can" && u.Abbreviation == "can" && u.IsActive && u.GarageID == garageID && *u.CreatedBy == actorID
		})).Return(nil).Once()

		unit, err := uc.CreateUnit(t.Context(), usecase.CreateUnitRequest{
			GarageID: garageID, ActorID: actorID, Fields: usecase.UnitFields{Name: new(" Can "), Abbreviation: new("can")},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, unit.ID)
	})

	t.Run("without an actor the audit columns stay empty", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateUnit(mock.Anything, mock.MatchedBy(func(u *domain.Unit) bool { return u.CreatedBy == nil })).Return(nil).Once()

		_, err := uc.CreateUnit(t.Context(), usecase.CreateUnitRequest{
			GarageID: garageID, Fields: usecase.UnitFields{Name: new("Can"), Abbreviation: new("can")},
		})
		require.NoError(t, err)
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		for name, fields := range map[string]usecase.UnitFields{
			"no name":         {Abbreviation: new("c")},
			"no abbreviation": {Name: new("Can")},
			"blank name":      {Name: new("  "), Abbreviation: new("c")},
			"blank abbrev":    {Name: new("Can"), Abbreviation: new(" ")},
			"name too long":   {Name: new(strings.Repeat("x", 51)), Abbreviation: new("c")},
			"abbrev too long": {Name: new("Can"), Abbreviation: new(strings.Repeat("x", 21))},
		} {
			_, err := uc.CreateUnit(t.Context(), usecase.CreateUnitRequest{GarageID: garageID, Fields: fields})
			assertAppError(t, err, apperror.CodeInvalidArgument)

			_ = name
		}
	})

	t.Run("duplicate name", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateUnit(mock.Anything, mock.Anything).Return(domain.ErrUnitNameTaken).Once()

		_, err := uc.CreateUnit(t.Context(), usecase.CreateUnitRequest{
			GarageID: garageID, Fields: usecase.UnitFields{Name: new("Can"), Abbreviation: new("can")},
		})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestUseCase_UpdateUnit(t *testing.T) {
	t.Parallel()

	t.Run("applies the given fields only", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UpdateUnit(mock.Anything, mock.MatchedBy(func(u *domain.Unit) bool {
			return u.Name == "Tin" && u.Abbreviation == "can" && !u.IsActive && *u.UpdatedBy == actorID
		})).Return(nil).Once()

		unit, err := uc.UpdateUnit(t.Context(), usecase.UpdateUnitRequest{
			GarageID: garageID, UnitID: unitID, ActorID: actorID, Fields: usecase.UnitFields{Name: new("Tin"), IsActive: new(false)},
		})
		require.NoError(t, err)
		assert.Equal(t, "Tin", unit.Name)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(nil, domain.ErrUnitNotFound).Once()

		_, err := uc.UpdateUnit(t.Context(), usecase.UpdateUnitRequest{GarageID: garageID, UnitID: unitID})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()

		_, err = uc.UpdateUnit(t.Context(), usecase.UpdateUnitRequest{GarageID: garageID, UnitID: unitID, Fields: usecase.UnitFields{Name: new("")}})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UpdateUnit(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateUnit(t.Context(), usecase.UpdateUnitRequest{GarageID: garageID, UnitID: unitID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_ListUnits(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().ListUnits(mock.Anything, garageID, usecase.UnitFilter{IncludeInactive: true, Limit: 10, Offset: 10}).
		Return([]*domain.Unit{sampleUnit()}, 11, nil).Once()

	page, err := uc.ListUnits(t.Context(), usecase.ListUnitsRequest{
		PageRequest: usecase.PageRequest{Page: 2, PageSize: 10}, GarageID: garageID, IncludeInactive: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 11, page.Total)
	assert.Equal(t, 2, page.Page)

	d.Repo.EXPECT().ListUnits(mock.Anything, garageID, usecase.UnitFilter{Limit: usecase.DefaultPageSize}).Return(nil, 0, nil).Once()

	page, err = uc.ListUnits(t.Context(), usecase.ListUnitsRequest{GarageID: garageID})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Page)

	d.Repo.EXPECT().ListUnits(mock.Anything, garageID, usecase.UnitFilter{Limit: usecase.MaxPageSize}).Return(nil, 0, errBoom).Once()

	_, err = uc.ListUnits(t.Context(), usecase.ListUnitsRequest{GarageID: garageID, PageRequest: usecase.PageRequest{PageSize: 5000}})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestUseCase_DeleteUnit(t *testing.T) {
	t.Parallel()

	req := usecase.DeleteRequest{GarageID: garageID, ID: unitID, ActorID: actorID}

	t.Run("an unused unit is deleted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UnitUsedByParts(mock.Anything, garageID, unitID).Return(false, nil).Once()
		d.Usage.EXPECT().UnitInUse(mock.Anything, garageID, unitID).Return(false, nil).Once()
		d.Repo.EXPECT().SoftDeleteUnit(mock.Anything, garageID, unitID, actorID).Return(nil).Once()

		outcome, err := uc.DeleteUnit(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
	})

	t.Run("a unit used by a part is deactivated", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UnitUsedByParts(mock.Anything, garageID, unitID).Return(true, nil).Once()
		d.Repo.EXPECT().SetUnitActive(mock.Anything, garageID, unitID, false, actorID).Return(nil).Once()

		outcome, err := uc.DeleteUnit(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("a unit used by a document is deactivated; dry run changes nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UnitUsedByParts(mock.Anything, garageID, unitID).Return(false, nil).Once()
		d.Usage.EXPECT().UnitInUse(mock.Anything, garageID, unitID).Return(true, nil).Once()

		dry := req
		dry.DryRun = true

		outcome, err := uc.DeleteUnit(t.Context(), dry)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(nil, domain.ErrUnitNotFound).Once()

		_, err := uc.DeleteUnit(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().UnitUsedByParts(mock.Anything, garageID, unitID).Return(false, errBoom).Once()

		_, err = uc.DeleteUnit(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
