package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

const (
	garageID      = "g-1"
	repairOrderID = "ro-1"
	vehicleID     = "v-1"
)

func newRepairOrder() *domain.RepairOrder {
	quoteID := "q-1"

	return &domain.RepairOrder{
		ID: repairOrderID, GarageID: garageID, QuoteID: &quoteID, VehicleID: vehicleID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "LSC-202609-0001", Status: domain.StatusOpen,
		Lines: []domain.Line{
			{
				ID: "l-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1",
				UnitPrice: 250_000, LineTotal: 250_000, Status: domain.LineNotStarted,
			},
			{
				ID: "l-2", Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1",
				UnitPrice: 480_000, LineTotal: 480_000, Status: domain.LineNotStarted,
			},
		},
	}
}

func repairOrderRow() []any {
	return []any{
		repairOrderID, garageID, "q-1", vehicleID, "51F-123.45", "An Nguyen", "LSC-202609-0001", "open",
		int64(730_000), []byte(`{"name":"Cong ty An Phat"}`), []byte(`{"tax_profile_id":"tp-1","legal_name":"Cong ty Van Tai A"}`),
		nil, nil, nil, now, now,
	}
}

func lineRow() []any {
	return []any{"l-1", 1, "labor", nil, "Oil change", "1", int64(250_000), int64(250_000), "not_started", []string{}}
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

func TestPostgresRepository_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM repair_orders WHERE garage_id", dbtest.Row(repairOrderRow()...)).
			ExpectQuery("FROM repair_order_lines", dbtest.NewRows(lineRow()), nil)
		ctx := tx.Context(t.Context())

		ro, err := repo.FindByID(ctx, garageID, repairOrderID)
		require.NoError(t, err)
		require.Equal(t, domain.StatusOpen, ro.Status)
		require.Len(t, ro.Lines, 1)
		require.Equal(t, "Oil change", ro.Lines[0].Description)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM repair_orders WHERE garage_id", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, "missing")
		require.ErrorIs(t, err, domain.ErrRepairOrderNotFound)
	})

	t.Run("unexpected read error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM repair_orders WHERE garage_id", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, repairOrderID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("lines query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM repair_orders WHERE garage_id", dbtest.Row(repairOrderRow()...)).
			ExpectQuery("FROM repair_order_lines", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, repairOrderID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("line scan failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM repair_orders WHERE garage_id", dbtest.Row(repairOrderRow()...)).
			ExpectQuery("FROM repair_order_lines", dbtest.NewRows(lineRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, repairOrderID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_Save(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		ro := newRepairOrder()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE repair_orders SET status", dbtest.Row(now)).
			ExpectExec("UPDATE repair_order_lines SET status", dbtest.Tag(1), nil).
			ExpectExec("UPDATE repair_order_lines SET status", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Save(ctx, ro))
		require.Equal(t, now, ro.UpdatedAt)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE repair_orders SET status", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Save(ctx, newRepairOrder()), domain.ErrRepairOrderNotFound)
	})

	t.Run("unexpected update error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE repair_orders SET status", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Save(ctx, newRepairOrder()), errBoom)
	})

	t.Run("line update fails", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE repair_orders SET status", dbtest.Row(now)).
			ExpectExec("UPDATE repair_order_lines SET status", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Save(ctx, newRepairOrder()), errBoom)
	})
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM repair_orders", dbtest.Row(1)).
			ExpectQuery("FROM repair_orders ro", dbtest.NewRows(repairOrderRow()), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM repair_orders", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, items)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM repair_orders", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("list query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM repair_orders", dbtest.Row(1)).
			ExpectQuery("FROM repair_orders ro", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_ListForVehicle(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	row := repairOrderRow()

	tx := dbtest.New(t).
		ExpectQuery("FROM repair_orders", dbtest.NewRows(row), nil).
		ExpectQuery("FROM repair_orders", nil, errBoom).
		ExpectQuery("FROM repair_orders", dbtest.NewRows(row).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	list, err := repo.ListForVehicle(ctx, garageID, vehicleID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, domain.StatusOpen, list[0].Status)
	require.Equal(t, int64(730_000), list[0].TotalAmount)

	_, err = repo.ListForVehicle(ctx, garageID, vehicleID)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.ListForVehicle(ctx, garageID, vehicleID)
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_ExistsForVehicles(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("FROM repair_orders", dbtest.Row(true)).
		ExpectQueryRow("FROM repair_orders", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	exists, err := repo.ExistsForVehicles(ctx, garageID, []string{vehicleID})
	require.NoError(t, err)
	require.True(t, exists)

	_, err = repo.ExistsForVehicles(ctx, garageID, []string{vehicleID})
	require.ErrorIs(t, err, errBoom)

	exists, err = repo.ExistsForVehicles(ctx, garageID, nil)
	require.NoError(t, err)
	require.False(t, exists, "no vehicle, no query")
}

func TestPostgresRepository_LiveVehicleIDs(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	ids, err := repo.LiveVehicleIDs(t.Context(), garageID, nil)
	require.NoError(t, err)
	require.Empty(t, ids)

	tx := dbtest.New(t).
		ExpectQuery("FROM repair_orders", dbtest.NewRows([]any{vehicleID}), nil).
		ExpectQuery("FROM repair_orders", nil, errBoom).
		ExpectQuery("FROM repair_orders", dbtest.NewRows([]any{errBoom}), nil)
	ctx := tx.Context(t.Context())

	ids, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.NoError(t, err)
	require.Equal(t, []string{vehicleID}, ids)

	_, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.ErrorIs(t, err, errBoom)

	_, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.Error(t, err)
}
