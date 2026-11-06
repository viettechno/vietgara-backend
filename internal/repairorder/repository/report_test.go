package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/repository"
)

func TestPostgresRepository_CompletedWork(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	from, to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	tx := dbtest.New(t).
		ExpectQuery("FROM repair_order_lines l", dbtest.NewRows([]any{"tech-1", 4, 2, int64(90)}), nil).
		ExpectQuery("FROM repair_order_lines l", nil, errBoom).
		ExpectQuery("FROM repair_order_lines l", dbtest.NewRows([]any{"tech-1", 4, 2, int64(90)}).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	work, err := repo.CompletedWork(ctx, garageID, "Asia/Ho_Chi_Minh", from, to)
	require.NoError(t, err)
	assert.Equal(t, []repository.TechnicianWork{{TechnicianID: "tech-1", CompletedLines: 4, RepairOrders: 2, AverageTurnaroundMinutes: 90}}, work)

	for range 2 {
		_, err = repo.CompletedWork(ctx, garageID, "UTC", from, to)
		require.ErrorIs(t, err, errBoom)
	}
}

func TestPostgresRepository_ProfitLines(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	from, to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	row := []any{"ro-1", "LSC-1", "l-1", "job-1", int64(240_000), true, []string{"tech-1"}}

	tx := dbtest.New(t).
		ExpectQuery("FROM repair_orders ro JOIN repair_order_lines l", dbtest.NewRows(row), nil).
		ExpectQuery("FROM repair_orders ro JOIN repair_order_lines l", nil, errBoom).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(true)).
		ExpectQuery("FROM repair_orders ro JOIN repair_order_lines l", dbtest.NewRows(row), nil).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(false)).
		ExpectQueryRow("SELECT EXISTS", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(true)).
		ExpectQuery("FROM repair_orders ro JOIN repair_order_lines l", nil, errBoom).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(true)).
		ExpectQuery("FROM repair_orders ro JOIN repair_order_lines l", dbtest.NewRows(row).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	want := []repository.ProfitLine{{
		RepairOrderID: "ro-1", RepairOrderNumber: "LSC-1", LineID: "l-1", JobID: "job-1", Revenue: 240_000, Consumes: true, TechnicianIDs: []string{"tech-1"},
	}}

	lines, err := repo.ClosedLines(ctx, garageID, "Asia/Ho_Chi_Minh", from, to)
	require.NoError(t, err)
	assert.Equal(t, want, lines)

	_, err = repo.ClosedLines(ctx, garageID, "UTC", from, to)
	require.ErrorIs(t, err, errBoom)

	lines, err = repo.OrderLines(ctx, garageID, "ro-1")
	require.NoError(t, err)
	assert.Equal(t, want, lines)

	_, err = repo.OrderLines(ctx, garageID, "ro-1")
	require.ErrorIs(t, err, domain.ErrRepairOrderNotFound)

	_, err = repo.OrderLines(ctx, garageID, "ro-1")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.OrderLines(ctx, garageID, "ro-1")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.OrderLines(ctx, garageID, "ro-1")
	require.ErrorIs(t, err, errBoom)
}
