package usecase_test

import (
	"errors"
	"testing"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"

	"github.com/stretchr/testify/require"
)

const (
	garageID = "11111111-1111-1111-1111-111111111111"
	partID   = "22222222-2222-2222-2222-222222222222"
	unitID   = "44444444-4444-4444-4444-444444444444"
	actorID  = "33333333-3333-3333-3333-333333333333"
)

var errBoom = errors.New("boom")

type deps struct {
	Repo   *mocks.MockInventoryRepository
	Parts  *mocks.MockInventoryPartCatalogue
	Events *mocks.MockInventoryEventPublisher
}

func newUseCase(t *testing.T) (*usecase.InventoryUseCase, *deps) {
	t.Helper()

	d := &deps{
		Repo: mocks.NewMockInventoryRepository(t), Parts: mocks.NewMockInventoryPartCatalogue(t),
		Events: mocks.NewMockInventoryEventPublisher(t),
	}

	return usecase.NewInventoryUseCase(d.Repo, d.Parts, d.Events), d
}

func samplePart() usecase.PartInfo {
	return usecase.PartInfo{
		ID: partID, SKU: "BEER-330", Name: "Beer 330 ml", BaseUnitName: "Can", BaseUnitAbbreviation: "can",
		CategoryName: "Beverages", Brand: "Tiger", IsActive: true, ReorderThreshold: "5",
	}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
