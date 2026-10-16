package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID = "11111111-1111-1111-1111-111111111111"
	partID   = "22222222-2222-2222-2222-222222222222"
	unitID   = "33333333-3333-3333-3333-333333333333"
	caseID   = "44444444-4444-4444-4444-444444444444"
	catID    = "55555555-5555-5555-5555-555555555555"
	fileID   = "66666666-6666-6666-6666-666666666666"
	actorID  = "77777777-7777-7777-7777-777777777777"
)

var errBoom = errors.New("boom")

type deps struct {
	Repo  *mocks.MockCatalogueRepository
	Usage *mocks.MockCatalogueUsageChecker
	Files *mocks.MockCatalogueFiles
}

func newUseCase(t *testing.T) (*usecase.UseCase, *deps) {
	t.Helper()

	d := &deps{
		Repo: mocks.NewMockCatalogueRepository(t), Usage: mocks.NewMockCatalogueUsageChecker(t),
		Files: mocks.NewMockCatalogueFiles(t),
	}

	return usecase.NewUseCase(d.Repo, d.Usage, d.Files), d
}

func sampleUnit() *domain.Unit {
	return &domain.Unit{ID: unitID, GarageID: garageID, Name: "Can", Abbreviation: "can", IsActive: true}
}

func sampleCase() *domain.Unit {
	return &domain.Unit{ID: caseID, GarageID: garageID, Name: "Carton", Abbreviation: "ctn", IsActive: true}
}

func samplePart() *domain.Part {
	return &domain.Part{
		ID: partID, GarageID: garageID, SKU: "BEER-330", Name: "Beer 330 ml", BaseUnitID: unitID, BaseUnitName: "Can",
		BaseUnitAbbreviation: "can", SalePrice: 15_000, Brand: "Tiger", IsActive: true, ReorderThreshold: "0",
	}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
