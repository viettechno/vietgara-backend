//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/repository"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

type world struct {
	pg      *pgtest.Postgres
	pool    *platformdb.Pool
	tx      *platformdb.TxManager
	repo    *repository.PostgresRepository
	owner   string
	garageA string
	garageB string
}

func newWorld(t *testing.T) *world {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	w := &world{pg: pg, owner: uuid.NewString(), garageA: uuid.NewString(), garageB: uuid.NewString()}

	_, err := pg.Admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@x.test', 'Owner')`, w.owner)
	require.NoError(t, err)

	for i, g := range []string{w.garageA, w.garageB} {
		_, err = pg.Admin.ExecContext(t.Context(), `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, $3)`, g, w.owner, []string{"A", "B"}[i])
		require.NoError(t, err)
	}

	w.pool = pg.AppPool(t)
	w.tx = platformdb.NewTxManager(w.pool)
	w.repo = repository.NewPostgresRepository(w.pool)

	return w
}

// in runs fn in a transaction scoped to the garage, like a request.
func (w *world) in(t *testing.T, garage string, fn func(ctx context.Context)) {
	t.Helper()

	require.NoError(t, w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: garage}, func(ctx context.Context) error {
		fn(ctx)

		return nil
	}))
}

func (w *world) newReceivable(t *testing.T, garage, number string, total, paid int64, due time.Time, customer string) *domain.Receivable {
	t.Helper()

	r := &domain.Receivable{
		GarageID: garage, SettlementID: uuid.NewString(), SettlementNumber: number, CustomerID: customer, CustomerName: "An",
		VehicleLicensePlate: "51A-111.11", TotalAmount: total, PaidAmount: paid, DueDate: due,
	}
	r.Derive()

	w.in(t, garage, func(ctx context.Context) { require.NoError(t, w.repo.Save(ctx, r)) })

	return r
}

func TestSettingsRoundTripAndConstraints(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	w.in(t, w.garageA, func(ctx context.Context) {
		s, err := w.repo.GetSettings(ctx, w.garageA)
		require.NoError(t, err)
		require.Equal(t, domain.DefaultSettings(), s)

		want := domain.Settings{PaymentTermsDays: 30, RemindersEnabled: false, ReminderOffsetDays: []int{1, 5, 90}}
		require.NoError(t, w.repo.SaveSettings(ctx, w.garageA, want))
		require.NoError(t, w.repo.SaveSettings(ctx, w.garageA, want), "saving again replaces")

		got, err := w.repo.GetSettings(ctx, w.garageA)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	w.in(t, w.garageB, func(ctx context.Context) {
		s, err := w.repo.GetSettings(ctx, w.garageB)
		require.NoError(t, err)
		require.Equal(t, domain.DefaultSettings(), s, "another garage's settings are not visible")
	})

	// The database refuses what the domain refuses.
	for _, bad := range []domain.Settings{
		{PaymentTermsDays: 366},
		{PaymentTermsDays: 7, ReminderOffsetDays: []int{0}},
		{PaymentTermsDays: 7, ReminderOffsetDays: []int{91}},
		{PaymentTermsDays: 7, ReminderOffsetDays: []int{1, 2, 3, 4, 5, 6}},
	} {
		err := w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
			return w.repo.SaveSettings(ctx, w.garageA, bad)
		})
		require.Error(t, err, bad)
	}
}

func TestSaveIsAnUpsertAndEntriesAreIdempotent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	due := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	r := w.newReceivable(t, w.garageA, "QT-1", 1_000_000, 0, due, "")
	first := r.ID

	require.NotEmpty(t, first)

	customer := uuid.NewString()

	_, err := w.pg.Admin.ExecContext(t.Context(), `
		INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customer, w.garageA)
	require.NoError(t, err)

	w.in(t, w.garageA, func(ctx context.Context) {
		again := *r
		again.CustomerID, again.PaidAmount, again.DueDate = customer, 400_000, due.AddDate(0, 0, 30)
		again.Derive()
		require.NoError(t, w.repo.Save(ctx, &again))
		require.Equal(t, first, again.ID, "the same settlement keeps its receivable")

		got, err := w.repo.FindBySettlement(ctx, w.garageA, r.SettlementID)
		require.NoError(t, err)
		require.Equal(t, customer, got.CustomerID)
		require.EqualValues(t, 400_000, got.PaidAmount)
		require.Equal(t, due, got.DueDate, "the due date is fixed when the receivable is created")

		// A later sync that cannot resolve the customer keeps the known one.
		blank := *got
		blank.CustomerID = ""
		require.NoError(t, w.repo.Save(ctx, &blank))

		kept, err := w.repo.FindBySettlement(ctx, w.garageA, r.SettlementID)
		require.NoError(t, err)
		require.Equal(t, customer, kept.CustomerID)

		entries := []domain.Entry{
			{Kind: domain.EntryCharge, Amount: 1_000_000, Reference: "QT-1", SourceKey: "charge:" + r.SettlementID, OccurredAt: due},
			{Kind: domain.EntryPayment, Amount: 400_000, Reference: "PT-1", Method: "cash", SourceKey: "payment:p-1", OccurredAt: due.Add(time.Hour)},
		}
		require.NoError(t, w.repo.AddEntries(ctx, w.garageA, first, entries))
		require.NoError(t, w.repo.AddEntries(ctx, w.garageA, first, entries), "a repeated sync adds nothing")

		full, err := w.repo.FindByID(ctx, w.garageA, first)
		require.NoError(t, err)
		require.Len(t, full.Entries, 2)
		require.Equal(t, "cash", full.Entries[1].Method)

		balance, err := w.repo.LedgerBalance(ctx, w.garageA)
		require.NoError(t, err)
		require.EqualValues(t, 600_000, balance)

		remaining, err := w.repo.RemainingTotal(ctx, w.garageA)
		require.NoError(t, err)
		require.EqualValues(t, 600_000, remaining)
	})
}

func TestRowLevelSecurityKeepsGaragesApart(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	due := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	a := w.newReceivable(t, w.garageA, "QT-A", 100, 0, due, "")
	w.newReceivable(t, w.garageB, "QT-B", 200, 0, due, "")

	w.in(t, w.garageB, func(ctx context.Context) {
		_, err := w.repo.FindByID(ctx, w.garageB, a.ID)
		require.ErrorIs(t, err, domain.ErrReceivableNotFound, "another garage's receivable cannot be read by id")

		items, total, summary, err := w.repo.List(ctx, w.garageB, usecase.ListFilter{Today: due, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Equal(t, "QT-B", items[0].SettlementNumber)
		require.EqualValues(t, 200, summary.OutstandingAmount)

		// Asking for another garage's rows from this garage's scope finds none.
		other, _, _, err := w.repo.List(ctx, w.garageA, usecase.ListFilter{Today: due, Limit: 50})
		require.NoError(t, err)
		require.Empty(t, other)
	})
}

func TestListFiltersOrderAndSummary(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	today := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	customer := uuid.NewString()

	_, err := w.pg.Admin.ExecContext(t.Context(), `INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customer, w.garageA)
	require.NoError(t, err)

	late := w.newReceivable(t, w.garageA, "QT-LATE", 1_000_000, 400_000, today.AddDate(0, 0, -5), customer)
	soon := w.newReceivable(t, w.garageA, "QT-SOON", 500_000, 0, today.AddDate(0, 0, 3), "")
	paid := w.newReceivable(t, w.garageA, "QT-100%", 300_000, 300_000, today.AddDate(0, 0, -9), "")

	w.in(t, w.garageA, func(ctx context.Context) {
		list := func(f usecase.ListFilter) ([]*domain.Receivable, int, usecase.Summary) {
			f.Today, f.Limit = today, 50
			items, total, sum, err := w.repo.List(ctx, w.garageA, f)
			require.NoError(t, err)

			return items, total, sum
		}

		items, total, sum := list(usecase.ListFilter{})
		require.Equal(t, 2, total, "paid receivables are left out")
		require.Equal(t, late.ID, items[0].ID, "the most overdue comes first")
		require.Equal(t, soon.ID, items[1].ID)
		require.Equal(t, usecase.Summary{OpenCount: 2, OutstandingAmount: 1_100_000, OverdueCount: 1, OverdueAmount: 600_000}, sum)

		items, total, _ = list(usecase.ListFilter{IncludePaid: true})
		require.Equal(t, 3, total)
		require.Equal(t, paid.ID, items[2].ID, "paid ones come last")

		items, _, _ = list(usecase.ListFilter{OverdueOnly: true})
		require.Len(t, items, 1)
		require.Equal(t, late.ID, items[0].ID)

		items, _, _ = list(usecase.ListFilter{CustomerID: customer})
		require.Len(t, items, 1)

		items, _, _ = list(usecase.ListFilter{Query: "soon"})
		require.Len(t, items, 1, "the search is case-insensitive")

		items, _, _ = list(usecase.ListFilter{Query: "100%", IncludePaid: true})
		require.Len(t, items, 1, "a percent sign in the search is matched literally")

		items, _, _ = list(usecase.ListFilter{Query: "%", IncludePaid: true})
		require.Len(t, items, 1, "and does not match everything")

		paged, total, _, err := w.repo.List(ctx, w.garageA, usecase.ListFilter{Today: today, Limit: 1, Offset: 1})
		require.NoError(t, err)
		require.Len(t, paged, 1)
		require.Equal(t, 2, total)
		require.Equal(t, soon.ID, paged[0].ID)
	})
}

func TestRemindersAndGarages(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	due := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	customer := uuid.NewString()

	_, err := w.pg.Admin.ExecContext(t.Context(), `INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customer, w.garageA)
	require.NoError(t, err)

	r := w.newReceivable(t, w.garageA, "QT-1", 1_000_000, 0, due, customer)
	w.newReceivable(t, w.garageB, "QT-2", 1_000_000, 0, due, "") // no customer: never reminded

	garages, err := w.repo.ReminderGarages(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{w.garageA}, garages, "only a garage with an open receivable of a known customer")

	w.in(t, w.garageA, func(ctx context.Context) {
		require.NoError(t, w.repo.SaveReminder(ctx, w.garageA, r.ID, &domain.Reminder{
			OffsetDays: 3, Channel: "email", Outcome: domain.OutcomeFailed, Attempts: 1,
		}))
		require.NoError(t, w.repo.SaveReminder(ctx, w.garageA, r.ID, &domain.Reminder{
			OffsetDays: 3, Channel: "email", Outcome: domain.OutcomeSent, Attempts: 2,
		}), "the same offset is updated, never duplicated")
		require.NoError(t, w.repo.SaveReminder(ctx, w.garageA, r.ID, &domain.Reminder{
			OffsetDays: 7, Channel: "email", Outcome: domain.OutcomeSkipped, Reason: domain.ReasonNoEmail, Attempts: 1,
		}))
		require.NoError(t, w.repo.MarkReminded(ctx, w.garageA, r.ID, time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)))

		open, err := w.repo.OpenWithCustomer(ctx, w.garageA, due.AddDate(0, 0, 3))
		require.NoError(t, err)
		require.Len(t, open, 1)
		require.Equal(t, 1, open[0].RemindersSent)
		require.Len(t, open[0].Reminders, 2)
		require.Equal(t, domain.OutcomeSent, open[0].Reminders[0].Outcome)
		require.Equal(t, 2, open[0].Reminders[0].Attempts)
		require.Equal(t, domain.ReasonNoEmail, open[0].Reminders[1].Reason)
	})

	// A paid receivable leaves the reminder garages.
	w.in(t, w.garageA, func(ctx context.Context) {
		paid := *r
		paid.PaidAmount = paid.TotalAmount
		paid.Derive()
		require.NoError(t, w.repo.Save(ctx, &paid))
	})

	garages, err = w.repo.ReminderGarages(t.Context())
	require.NoError(t, err)
	require.Empty(t, garages)
}

func TestAdvisoryLockIsExclusive(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
			ok, err := w.repo.TryLock(ctx, "receivable-reminder:x")
			if err != nil || !ok {
				t.Error("the first instance must get the lock")
			}

			close(held)
			<-release

			return nil
		})
	}()

	<-held

	w.in(t, w.garageA, func(ctx context.Context) {
		ok, err := w.repo.TryLock(ctx, "receivable-reminder:x")
		require.NoError(t, err)
		require.False(t, ok, "a second instance is told the lock is taken")

		other, err := w.repo.TryLock(ctx, "receivable-reminder:y")
		require.NoError(t, err)
		require.True(t, other, "another receivable is free")
	})

	close(release)
	<-done

	w.in(t, w.garageA, func(ctx context.Context) {
		ok, err := w.repo.TryLock(ctx, "receivable-reminder:x")
		require.NoError(t, err)
		require.True(t, ok, "the lock ends with the transaction")
	})
}

func TestLedgerPeriodQueries(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	day := func(d int) time.Time { return time.Date(2026, 11, d, 10, 0, 0, 0, time.UTC) }
	r := w.newReceivable(t, w.garageA, "QT-1", 1_000_000, 400_000, day(20), "")

	w.in(t, w.garageA, func(ctx context.Context) {
		require.NoError(t, w.repo.AddEntries(ctx, w.garageA, r.ID, []domain.Entry{
			{Kind: domain.EntryCharge, Amount: 1_000_000, Reference: "QT-1", SourceKey: "c", OccurredAt: day(2)},
			{Kind: domain.EntryPayment, Amount: 700_000, Reference: "PT-1", Method: "cash", SourceKey: "p1", OccurredAt: day(5)},
			{Kind: domain.EntryVoid, Amount: 700_000, Reference: "PT-1", Method: "cash", SourceKey: "v1", OccurredAt: day(6)},
			{Kind: domain.EntryPayment, Amount: 400_000, Reference: "PT-2", Method: "bank_transfer", SourceKey: "p2", OccurredAt: day(7)},
		}))

		before, err := w.repo.BalanceBefore(ctx, w.garageA, day(5))
		require.NoError(t, err)
		require.EqualValues(t, 1_000_000, before)

		lines, err := w.repo.Entries(ctx, w.garageA, day(5), day(7))
		require.NoError(t, err)
		require.Len(t, lines, 2, "[from, to) excludes the end")
		require.Equal(t, domain.EntryPayment, lines[0].Entry.Kind)
		require.Equal(t, domain.EntryVoid, lines[1].Entry.Kind)
		require.Equal(t, "An", lines[0].CustomerName)

		balance, err := w.repo.LedgerBalance(ctx, w.garageA)
		require.NoError(t, err)
		require.EqualValues(t, 600_000, balance)

		remaining, err := w.repo.RemainingTotal(ctx, w.garageA)
		require.NoError(t, err)
		require.EqualValues(t, 600_000, remaining, "the ledger agrees with the receivable")
	})
}

func TestMissingSettlementsAndSettlementLock(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := t.Context()

	// Settlements need a repair order; the function only reads garage_id, id and
	// status, so the rows are inserted without their references.
	tx, err := w.pg.Admin.BeginTx(ctx, nil)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`)
	require.NoError(t, err)

	var open, paid, covered string

	for target, status := range map[*string]string{&open: "pending", &paid: "paid", &covered: "partially_paid"} {
		id := uuid.NewString()
		*target = id

		_, err = tx.ExecContext(ctx, `
			INSERT INTO settlements (id, garage_id, repair_order_id, customer_name, vehicle_license_plate, number, total_amount, status)
			VALUES ($1, $2, $3, 'An', '51A', $4, 100, $5::settlement_status)`, id, w.garageA, uuid.NewString(), "N-"+id[:6], status)
		require.NoError(t, err)
	}

	require.NoError(t, tx.Commit())

	w.in(t, w.garageA, func(ctx context.Context) {
		require.NoError(t, w.repo.Save(ctx, &domain.Receivable{
			GarageID: w.garageA, SettlementID: covered, SettlementNumber: "N", CustomerName: "An", VehicleLicensePlate: "51A",
			TotalAmount: 100, PaidAmount: 10, DueDate: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC), Status: domain.StatusOpen,
		}))
	})

	missing, err := w.repo.MissingSettlements(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []usecase.MissingSettlement{{GarageID: w.garageA, SettlementID: open}}, missing,
		"only the unpaid settlement without a receivable; a paid one needs none")

	limited, err := w.repo.MissingSettlements(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, limited)

	// Two syncs of one settlement run one after the other.
	held := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)

		_ = w.tx.WithTransaction(ctx, platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
			if err := w.repo.LockSettlement(ctx, open); err != nil {
				t.Error(err)
			}

			close(held)
			<-release

			return nil
		})
	}()

	<-held

	second := make(chan struct{})

	go func() {
		defer close(second)

		_ = w.tx.WithTransaction(ctx, platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
			return w.repo.LockSettlement(ctx, open)
		})
	}()

	select {
	case <-second:
		t.Fatal("the second sync got the lock while the first held it")
	case <-time.After(500 * time.Millisecond):
	}

	close(release)
	<-finished

	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("the second sync never got the lock")
	}
}
