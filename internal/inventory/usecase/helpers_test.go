package usecase_test

import (
	"errors"
	"testing"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"

	"github.com/stretchr/testify/require"
)

const (
	garageID = "11111111-1111-1111-1111-111111111111"
	itemID   = "22222222-2222-2222-2222-222222222222"
	actorID  = "33333333-3333-3333-3333-333333333333"
)

var errBoom = errors.New("boom")

type deps struct {
	Repo   *mocks.MockInventoryRepository
	Events *mocks.MockInventoryEventPublisher
}

func newUseCase(t *testing.T) (*usecase.InventoryUseCase, *deps) {
	t.Helper()

	d := &deps{Repo: mocks.NewMockInventoryRepository(t), Events: mocks.NewMockInventoryEventPublisher(t)}
	uc := usecase.NewInventoryUseCase(d.Repo, d.Events)

	return uc, d
}

func sampleItem() *domain.Item {
	return &domain.Item{
		ID: itemID, GarageID: garageID, SKU: "OIL-5W30", Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5,
		QuantityOnHand: "10",
	}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
