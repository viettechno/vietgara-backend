//go:build integration

package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/repository"
)

// reportFixture seeds repair orders the way the reports read them: rows are
// inserted without their references (replica mode skips the foreign keys and
// the audit triggers).
type reportFixture struct {
	t      *testing.T
	pg     *pgtest.Postgres
	repo   *repository.PostgresRepository
	pool   *platformdb.Pool
	owner  string
	garage string
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	f := &reportFixture{t: t, pg: pg, owner: uuid.NewString(), garage: uuid.NewString()}
	ctx := t.Context()

	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@x.test', 'Owner')`, f.owner)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'Gara')`, f.garage, f.owner)
	require.NoError(t, err)

	f.pool = pg.AppPool(t)
	f.repo = repository.NewPostgresRepository(f.pool)

	return f
}

func (f *reportFixture) exec(query string, args ...any) {
	f.t.Helper()

	tx, err := f.pg.Admin.BeginTx(f.t.Context(), nil)
	require.NoError(f.t, err)

	_, err = tx.ExecContext(f.t.Context(), `SET LOCAL session_replication_role = replica`)
	require.NoError(f.t, err)

	_, err = tx.ExecContext(f.t.Context(), query, args...)
	require.NoError(f.t, err)
	require.NoError(f.t, tx.Commit())
}

func (f *reportFixture) order(number, status string, closedAt *time.Time) string {
	id := uuid.NewString()
	f.exec(`INSERT INTO repair_orders (id, garage_id, vehicle_id, customer_name, vehicle_license_plate, number, status, closed_at)
		VALUES ($1, $2, $3, 'Customer', '30A-12345', $4, $5::repair_order_status, $6)`,
		id, f.garage, uuid.NewString(), number, status, closedAt)

	return id
}

func (f *reportFixture) line(orderID, kind, status, nonBillable string, total int64, startedAt, completedAt *time.Time, job *string, techs ...string) string {
	id := uuid.NewString()
	f.exec(`INSERT INTO repair_order_lines (id, garage_id, repair_order_id, item_type, description, line_total, status,
			assigned_technicians, job_id, non_billable, started_at, completed_at)
		VALUES ($1, $2, $3, $4::quote_item_type, 'Line', $5, $6::repair_order_line_status, $7::uuid[], $8, $9::boolean, $10, $11)`,
		id, f.garage, orderID, kind, total, status, "{"+join(techs)+"}", job, nonBillable, startedAt, completedAt)

	return id
}

func join(ids []string) string { return strings.Join(ids, ",") }

func (f *reportFixture) in(fn func(ctx context.Context)) {
	f.t.Helper()

	require.NoError(f.t, platformdb.NewTxManager(f.pool).WithTransaction(f.t.Context(), platformdb.Scope{UserID: f.owner, GarageID: f.garage},
		func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
}

func at(day, clock string) *time.Time {
	t, err := time.Parse("2006-01-02 15:04", day+" "+clock)
	if err != nil {
		panic(err)
	}

	return &t
}

func TestCompletedWork_CountsLinesOrdersAndTurnaround(t *testing.T) {
	t.Parallel()

	f := newReportFixture(t)
	a, b := uuid.NewString(), uuid.NewString()
	r1, r2, r3 := f.order("R1", "closed", at("2026-11-10", "09:00")), f.order("R2", "completed", nil), f.order("R3", "in_progress", nil)
	cancelled := f.order("RX", "cancelled", nil)

	done := func(order string, start, end *time.Time, techs ...string) {
		f.line(order, "labor", "completed", "false", 100, start, end, nil, techs...)
	}

	// Two lines of A alone (60 and 120 minutes), one shared line (30 minutes) on R1.
	done(r1, at("2026-11-10", "08:00"), at("2026-11-10", "09:00"), a)
	done(r1, at("2026-11-10", "08:00"), at("2026-11-10", "10:00"), a)
	done(r1, at("2026-11-10", "08:30"), at("2026-11-10", "09:00"), a, b)
	// R2: one line of A with no start, one of B (45 minutes).
	done(r2, nil, at("2026-11-12", "10:00"), a)
	done(r2, at("2026-11-12", "09:15"), at("2026-11-12", "10:00"), b)
	// R3: a line of B that is not completed, a hidden consumable, and last month's line of A.
	f.line(r3, "labor", "in_progress", "false", 100, at("2026-11-12", "09:00"), nil, nil, b)
	f.line(r3, "part", "completed", "true", 0, nil, at("2026-11-12", "10:00"), nil, b)
	done(r3, at("2026-10-30", "08:00"), at("2026-10-30", "09:00"), a)
	// A cancelled order does not count; nor does a line completed after midnight 1 December in Vietnam (UTC 17:30 on the 30th).
	done(cancelled, at("2026-11-13", "08:00"), at("2026-11-13", "09:00"), a)
	done(r1, at("2026-11-30", "16:00"), at("2026-11-30", "17:30"), b)

	f.in(func(ctx context.Context) {
		work, err := f.repo.CompletedWork(ctx, f.garage, "Asia/Ho_Chi_Minh", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)

		got := map[string]repository.TechnicianWork{}
		for _, w := range work {
			got[w.TechnicianID] = w
		}

		// A: 4 lines (the shared one counts) on 2 orders; turnaround over the 3 lines that have a start: (60+120+30)/3 = 70.
		assert.Equal(t, repository.TechnicianWork{TechnicianID: a, CompletedLines: 4, RepairOrders: 2, AverageTurnaroundMinutes: 70}, got[a])
		// B: the shared line, R2's line and the line that ended after midnight in Vietnam (which is December there): 2 lines.
		assert.Equal(t, 2, got[b].CompletedLines)
		assert.Equal(t, 2, got[b].RepairOrders)
		assert.Equal(t, int64(38), got[b].AverageTurnaroundMinutes, "(30+45)/2 rounded")

		// Last month: only A's line of R3.
		last, err := f.repo.CompletedWork(ctx, f.garage, "Asia/Ho_Chi_Minh", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Len(t, last, 1)
		assert.Equal(t, a, last[0].TechnicianID)

		empty, err := f.repo.CompletedWork(ctx, f.garage, "UTC", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.Empty(t, empty)
	})
}

func TestProfitLines_ClosedInThePeriodInTheGarageZone(t *testing.T) {
	t.Parallel()

	f := newReportFixture(t)
	job := uuid.NewString()
	tech := uuid.NewString()

	// Closed at 23:30 on 30 November in Vietnam (16:30 UTC): in November. At 00:30 on 1 December: not.
	late := f.order("LATE", "closed", at("2026-11-30", "16:30"))
	next := f.order("NEXT", "closed", at("2026-11-30", "17:30"))
	open := f.order("OPEN", "open", nil)
	l1 := f.line(late, "labor", "completed", "false", 300_000, nil, nil, &job, tech)
	l2 := f.line(late, "part", "completed", "true", 999, nil, nil, &job)
	f.line(next, "labor", "completed", "false", 1, nil, nil, nil)
	f.line(open, "labor", "not_started", "false", 5, nil, nil, nil)

	f.in(func(ctx context.Context) {
		lines, err := f.repo.ClosedLines(ctx, f.garage, "Asia/Ho_Chi_Minh", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Len(t, lines, 2)
		assert.Equal(t, repository.ProfitLine{
			RepairOrderID: late, RepairOrderNumber: "LATE", LineID: l1, JobID: job, Revenue: 300_000, TechnicianIDs: []string{tech},
		}, lines[0])
		assert.Zero(t, lines[1].Revenue, "a hidden consumable has no revenue")
		assert.True(t, lines[1].Consumes)
		assert.Equal(t, l2, lines[1].LineID)

		one, err := f.repo.OrderLines(ctx, f.garage, open)
		require.NoError(t, err)
		require.Len(t, one, 1)
		assert.Equal(t, int64(5), one[0].Revenue)

		_, err = f.repo.OrderLines(ctx, f.garage, uuid.NewString())
		require.ErrorIs(t, err, domain.ErrRepairOrderNotFound)
	})

	other := uuid.NewString()
	f.exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'o2@x.test', 'O2')`, other)

	// Another garage sees none of it.
	foreign := uuid.NewString()
	f.exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'Other')`, foreign, other)
	require.NoError(t, platformdb.NewTxManager(f.pool).WithTransaction(t.Context(), platformdb.Scope{UserID: other, GarageID: foreign},
		func(ctx context.Context) error {
			lines, err := f.repo.ClosedLines(ctx, foreign, "UTC", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
			assert.Empty(t, lines)
			require.NoError(t, err)

			_, err = f.repo.OrderLines(ctx, foreign, late)
			assert.ErrorIs(t, err, domain.ErrRepairOrderNotFound)

			return nil
		}))
}

func TestLineTimes_FollowTheStatusOfTheLine(t *testing.T) {
	t.Parallel()

	f := newReportFixture(t)
	orderID := f.order("R1", "open", nil)
	lineID := f.line(orderID, "labor", "not_started", "false", 100, nil, nil, nil)

	times := func() (started, completed *time.Time) {
		t.Helper()

		require.NoError(t, f.pg.Admin.QueryRowContext(t.Context(), `SELECT started_at, completed_at FROM repair_order_lines WHERE id = $1`, lineID).Scan(&started, &completed))

		return started, completed
	}

	save := func(status domain.LineStatus, replace bool) {
		t.Helper()

		ro := &domain.RepairOrder{
			ID: orderID, GarageID: f.garage, Status: domain.StatusInProgress, TotalAmount: 100,
			Lines: []domain.Line{{ID: lineID, Position: 1, ItemType: domain.ItemTypeLabor, Description: "Line", Quantity: "1", LineTotal: 100, Status: status}},
		}

		f.in(func(ctx context.Context) {
			if replace {
				require.NoError(t, f.repo.ReplaceLines(ctx, ro))
			} else {
				require.NoError(t, f.repo.Save(ctx, ro))
			}
		})
	}

	started, completed := times()
	assert.Nil(t, started)
	assert.Nil(t, completed)

	save(domain.LineInProgress, false)

	started, completed = times()
	require.NotNil(t, started, "the line first left not_started")
	assert.Nil(t, completed)

	first := *started

	time.Sleep(5 * time.Millisecond)
	save(domain.LineCompleted, true)

	started, completed = times()
	assert.Equal(t, first, *started, "the start does not move")
	require.NotNil(t, completed)

	done := *completed

	time.Sleep(5 * time.Millisecond)
	save(domain.LineCompleted, false)

	_, completed = times()
	assert.Equal(t, done, *completed, "completing again keeps the first completion")

	save(domain.LineInProgress, false)

	_, completed = times()
	assert.Nil(t, completed, "a reopened line is not completed")

	// A line inserted already completed (for example a copy) starts and completes at once.
	fresh := uuid.NewString()
	ro := &domain.RepairOrder{ID: orderID, GarageID: f.garage, Status: domain.StatusInProgress, Lines: []domain.Line{
		{ID: lineID, Position: 1, ItemType: domain.ItemTypeLabor, Description: "Line", Quantity: "1", LineTotal: 100, Status: domain.LineInProgress},
		{ID: fresh, Position: 2, ItemType: domain.ItemTypeLabor, Description: "New", Quantity: "1", LineTotal: 50, Status: domain.LineCompleted},
	}}
	f.in(func(ctx context.Context) { require.NoError(t, f.repo.ReplaceLines(ctx, ro)) })

	var s, c *time.Time

	require.NoError(t, f.pg.Admin.QueryRowContext(t.Context(), `SELECT started_at, completed_at FROM repair_order_lines WHERE id = $1`, fresh).Scan(&s, &c))
	assert.NotNil(t, s)
	assert.NotNil(t, c)
}
