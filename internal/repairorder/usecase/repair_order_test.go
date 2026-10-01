package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

type fakeRepo struct {
	created   *domain.RepairOrder
	createErr error
	list      []*domain.RepairOrder
	listErr   error
}

func (f *fakeRepo) Create(_ context.Context, ro *domain.RepairOrder) error {
	if f.createErr != nil {
		return f.createErr
	}

	ro.CreatedAt = time.Now()
	ro.UpdatedAt = ro.CreatedAt
	f.created = ro

	return nil
}

func (f *fakeRepo) ListForVehicle(_ context.Context, _, _ string) ([]*domain.RepairOrder, error) {
	return f.list, f.listErr
}

func TestCreateFromQuote(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := usecase.NewRepairOrderUseCase(repo)

	lines := []domain.Line{
		{Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000},
		{Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000},
	}

	ro, err := uc.CreateFromQuote(t.Context(), usecase.CreateFromQuoteRequest{
		GarageID: "g-1", QuoteID: "q-1", ActorID: "acc-1", VehicleID: "v-1", Number: "LSC-202609-0001", Lines: lines,
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusOpen, ro.Status)
	require.Equal(t, int64(730_000), ro.TotalAmount)
	require.Equal(t, "q-1", *ro.QuoteID)
	require.Len(t, ro.Lines, 2)
	require.NotEmpty(t, ro.ID)
	require.NotEmpty(t, ro.Lines[0].ID)
	require.Same(t, ro, repo.created)

	repo.createErr = domain.ErrAlreadyConverted
	_, err = uc.CreateFromQuote(t.Context(), usecase.CreateFromQuoteRequest{GarageID: "g-1", QuoteID: "q-1", VehicleID: "v-1"})
	require.ErrorIs(t, err, domain.ErrAlreadyConverted)
}

func TestCreateFromQuote_NoActor(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := usecase.NewRepairOrderUseCase(repo)

	ro, err := uc.CreateFromQuote(t.Context(), usecase.CreateFromQuoteRequest{GarageID: "g-1", QuoteID: "q-1", VehicleID: "v-1"})
	require.NoError(t, err)
	require.Nil(t, ro.CreatedBy)
	require.Nil(t, ro.UpdatedBy)
	require.Empty(t, ro.Lines)
	require.Equal(t, int64(0), ro.TotalAmount)
}

func TestListForVehicle(t *testing.T) {
	t.Parallel()

	want := []*domain.RepairOrder{{ID: "ro-1"}}
	repo := &fakeRepo{list: want}
	uc := usecase.NewRepairOrderUseCase(repo)

	got, err := uc.ListForVehicle(t.Context(), "g-1", "v-1")
	require.NoError(t, err)
	require.Equal(t, want, got)

	repo.listErr = errBoom
	_, err = uc.ListForVehicle(t.Context(), "g-1", "v-1")
	require.ErrorIs(t, err, errBoom)
}

var errBoom = errors.New("boom")
