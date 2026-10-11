package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/repository"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

const (
	garageID      = "g-1"
	repairOrderID = "ro-1"
	settlementID  = "s-1"
)

func newSettlement() *domain.Settlement {
	return &domain.Settlement{
		ID: settlementID, GarageID: garageID, RepairOrderID: repairOrderID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "QT-202609-0001", TotalAmount: 1_000_000,
		Status: domain.StatusPending,
	}
}

func settlementRow() []any {
	return []any{
		settlementID, garageID, repairOrderID, "51F-123.45", "An Nguyen", "QT-202609-0001", int64(1_000_000),
		int64(0), "pending", nil, nil, now, now,
	}
}

func paymentRow() []any {
	return []any{"p-1", int64(300_000), "cash", nil, nil, now}
}

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		s := newSettlement()

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO settlements", dbtest.Row(now, now))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, s))
		require.Equal(t, now, s.CreatedAt)
		require.Equal(t, now, s.UpdatedAt)
	})

	t.Run("repair order already settled", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO settlements", dbtest.ErrRow(dbtest.UniqueViolation("settlements_repair_order_id_unique")))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, newSettlement()), domain.ErrAlreadySettled)
	})

	t.Run("unexpected insert error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO settlements", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, newSettlement()), errBoom)
	})
}

func TestPostgresRepository_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("success, with payments", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.Row(settlementRow()...)).
			ExpectQuery("FROM settlement_payments", dbtest.NewRows(paymentRow()), nil)
		ctx := tx.Context(t.Context())

		s, err := repo.FindByID(ctx, garageID, settlementID)
		require.NoError(t, err)
		require.Equal(t, domain.StatusPending, s.Status)
		require.Len(t, s.Payments, 1)
		require.Equal(t, domain.MethodCash, s.Payments[0].Method)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, "missing")
		require.ErrorIs(t, err, domain.ErrSettlementNotFound)
	})

	t.Run("unexpected read error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, settlementID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("payments query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.Row(settlementRow()...)).
			ExpectQuery("FROM settlement_payments", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, settlementID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_FindByRepairOrderID(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.Row(settlementRow()...))
		ctx := tx.Context(t.Context())

		s, err := repo.FindByRepairOrderID(ctx, garageID, repairOrderID)
		require.NoError(t, err)
		require.Equal(t, settlementID, s.ID)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM settlements WHERE garage_id", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByRepairOrderID(ctx, garageID, "missing")
		require.ErrorIs(t, err, domain.ErrSettlementNotFound)
	})
}

func TestPostgresRepository_SaveStatus(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectExec("UPDATE settlements SET status", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.SaveStatus(ctx, garageID, settlementID, domain.StatusPaid))
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectExec("UPDATE settlements SET status", dbtest.Tag(0), nil)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.SaveStatus(ctx, garageID, "missing", domain.StatusPaid), domain.ErrSettlementNotFound)
	})

	t.Run("unexpected update error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectExec("UPDATE settlements SET status", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.SaveStatus(ctx, garageID, settlementID, domain.StatusPaid), errBoom)
	})
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM settlements", dbtest.Row(1)).
			ExpectQuery("FROM settlements", dbtest.NewRows(settlementRow()), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM settlements", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, items)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM settlements", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("list query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM settlements", dbtest.Row(1)).
			ExpectQuery("FROM settlements", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_RecordPayment(t *testing.T) {
	t.Parallel()

	t.Run("success, applied to the paid total", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		payment := &domain.Payment{ID: "p-1", Amount: 300_000, Method: domain.MethodCash}

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO settlement_payments", dbtest.Row(now)).
			ExpectQueryRow("UPDATE settlements SET paid_amount", dbtest.Row(settlementRow()...))
		ctx := tx.Context(t.Context())

		s, err := repo.RecordPayment(ctx, garageID, settlementID, payment)
		require.NoError(t, err)
		require.Equal(t, now, payment.CreatedAt)
		require.Equal(t, settlementID, s.ID)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		payment := &domain.Payment{ID: "p-1", Amount: 300_000, Method: domain.MethodCash}

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO settlement_payments", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, err := repo.RecordPayment(ctx, garageID, settlementID, payment)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("running total update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		payment := &domain.Payment{ID: "p-1", Amount: 300_000, Method: domain.MethodCash}

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO settlement_payments", dbtest.Row(now)).
			ExpectQueryRow("UPDATE settlements SET paid_amount", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, err := repo.RecordPayment(ctx, garageID, settlementID, payment)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_RevenueByPeriod(t *testing.T) {
	t.Parallel()

	from, to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("date_trunc", dbtest.NewRows([]any{now, int64(1_000_000)}), nil)
		ctx := tx.Context(t.Context())

		buckets, err := repo.RevenueByPeriod(ctx, garageID, "day", from, to)
		require.NoError(t, err)
		require.Equal(t, []repository.RevenueBucket{{PeriodStart: now, Amount: 1_000_000}}, buckets)
	})

	t.Run("no settlements in range", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("date_trunc", dbtest.NewRows(), nil)
		ctx := tx.Context(t.Context())

		buckets, err := repo.RevenueByPeriod(ctx, garageID, "month", from, to)
		require.NoError(t, err)
		require.Empty(t, buckets)
	})

	t.Run("rejects an invalid period unit before it reaches SQL", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)

		_, err := repo.RevenueByPeriod(t.Context(), garageID, "week", from, to)
		require.Error(t, err)
	})

	t.Run("query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("date_trunc", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, err := repo.RevenueByPeriod(ctx, garageID, "day", from, to)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("scan failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("date_trunc", dbtest.NewRows([]any{now, int64(1_000_000)}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.RevenueByPeriod(ctx, garageID, "day", from, to)
		require.ErrorIs(t, err, errBoom)
	})
}
