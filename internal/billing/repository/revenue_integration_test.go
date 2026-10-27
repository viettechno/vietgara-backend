//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestRevenueByPeriodInTheGarageTimeZone runs the revenue query on a real
// database: a settlement belongs to the day, month, quarter or year of the
// garage's time zone, not of UTC (FR-RPT-01, FR-TEN-05).
func TestRevenueByPeriodInTheGarageTimeZone(t *testing.T) {
	t.Parallel()

	pg := pgtest.StartMigrated(t)
	ctx := t.Context()

	owner, garage := uuid.NewString(), uuid.NewString()
	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@x.test', 'Owner')`, owner)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'Gara')`, garage, owner)
	require.NoError(t, err)

	// Settlements need a repair order; the query only reads garage_id,
	// created_at and total_amount, so the rows are inserted without their
	// references (replica mode skips the foreign key and audit triggers).
	tx, err := pg.Admin.BeginTx(ctx, nil)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`)
	require.NoError(t, err)

	for i, s := range []struct {
		at     time.Time
		amount int64
	}{
		{time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC), 100_000},   // 01:00 on 1 October in Vietnam
		{time.Date(2026, 10, 27, 16, 30, 0, 0, time.UTC), 200_000}, // 23:30 on 27 October
		{time.Date(2026, 10, 27, 17, 30, 0, 0, time.UTC), 500_000}, // 00:30 on 28 October
	} {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO settlements (garage_id, repair_order_id, customer_name, vehicle_license_plate, number, total_amount, created_at)
			VALUES ($1, $2, 'Customer', '30A-12345', $3, $4, $5)`,
			garage, uuid.NewString(), "TT-"+string(rune('1'+i)), s.amount, s.at)
		require.NoError(t, err)
	}

	require.NoError(t, tx.Commit())

	pool := pg.AppPool(t)
	repo := repository.NewPostgresRepository(pool)
	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)

	revenue := func(unit, zone string, from, to time.Time) map[string]int64 {
		t.Helper()

		got := map[string]int64{}

		require.NoError(t, platformdb.NewTxManager(pool).WithTransaction(ctx, platformdb.Scope{UserID: owner, GarageID: garage},
			func(ctx context.Context) error {
				buckets, err := repo.RevenueByPeriod(ctx, garage, unit, zone, from, to)
				for _, b := range buckets {
					got[b.PeriodStart.Format("2006-01-02")] = b.Amount
				}

				return err
			}))

		return got
	}

	from, to := time.Date(2026, 10, 27, 0, 0, 0, 0, hcm), time.Date(2026, 10, 29, 0, 0, 0, 0, hcm)

	require.Equal(t, map[string]int64{"2026-10-27": 200_000, "2026-10-28": 500_000},
		revenue("day", "Asia/Ho_Chi_Minh", from, to), "the sale after midnight in Vietnam is on the 28th")
	require.Equal(t, map[string]int64{"2026-10-27": 700_000},
		revenue("day", "UTC", from, to), "bucketed in UTC, both sales fall on the 27th (the bug this fixes)")

	from, to = time.Date(2026, 9, 1, 0, 0, 0, 0, hcm), time.Date(2026, 11, 1, 0, 0, 0, 0, hcm)

	require.Equal(t, map[string]int64{"2026-10-01": 800_000},
		revenue("month", "Asia/Ho_Chi_Minh", from, to), "01:00 on 1 October counts in October")
}
