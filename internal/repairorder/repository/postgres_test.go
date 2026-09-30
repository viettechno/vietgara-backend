package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/repository"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func newRepairOrder() *domain.RepairOrder {
	quoteID := "q-1"

	return &domain.RepairOrder{
		ID: "ro-1", GarageID: "g-1", QuoteID: &quoteID, VehicleID: "v-1", Number: "LSC-202609-0001", Status: domain.StatusOpen,
		Lines: []domain.Line{
			{ID: "l-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000},
			{ID: "l-2", Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000},
		},
	}
}

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		ro := newRepairOrder()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO repair_orders", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO repair_order_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO repair_order_lines", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, ro))
		require.Equal(t, now, ro.CreatedAt)
		require.Equal(t, now, ro.UpdatedAt)
	})

	t.Run("quote already converted", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO repair_orders", dbtest.ErrRow(dbtest.UniqueViolation("repair_orders_quote_id_unique")))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, newRepairOrder()), domain.ErrAlreadyConverted)
	})

	t.Run("unexpected insert error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO repair_orders", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, newRepairOrder()), errBoom)
	})

	t.Run("line insert fails", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO repair_orders", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO repair_order_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, newRepairOrder()), errBoom)
	})
}

func TestPostgresRepository_ListForVehicle(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	quoteID := "q-1"
	row := []any{"ro-1", "g-1", quoteID, "v-1", "LSC-202609-0001", "open", int64(730_000), nil, now, now}

	tx := dbtest.New(t).
		ExpectQuery("FROM repair_orders", dbtest.NewRows(row), nil).
		ExpectQuery("FROM repair_orders", nil, errBoom).
		ExpectQuery("FROM repair_orders", dbtest.NewRows(row).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	list, err := repo.ListForVehicle(ctx, "g-1", "v-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, domain.StatusOpen, list[0].Status)
	require.Equal(t, int64(730_000), list[0].TotalAmount)

	_, err = repo.ListForVehicle(ctx, "g-1", "v-1")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.ListForVehicle(ctx, "g-1", "v-1")
	require.ErrorIs(t, err, errBoom)
}
