package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/repository"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

var (
	now     = time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	due     = time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

const (
	garageID     = "g-1"
	receivableID = "r-1"
	settlementID = "s-1"
)

func receivableRow() []any {
	return []any{
		receivableID, garageID, settlementID, "QT-0001", new("c-1"), "An", "51A-111.11", int64(1_000_000), int64(400_000), due,
		"open", 1, &now, now, now,
	}
}

func repo() *repository.PostgresRepository { return repository.NewPostgresRepository(nil) }

func TestSettings(t *testing.T) {
	t.Parallel()

	t.Run("none saved gives the defaults", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM receivable_settings", dbtest.ErrRow(pgxNoRows()))
		s, err := repo().GetSettings(tx.Context(t.Context()), garageID)
		require.NoError(t, err)
		require.Equal(t, domain.DefaultSettings(), s)
	})

	t.Run("reads the saved ones", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM receivable_settings", dbtest.Row(14, false, []int32{2, 9}))
		s, err := repo().GetSettings(tx.Context(t.Context()), garageID)
		require.NoError(t, err)
		require.Equal(t, domain.Settings{PaymentTermsDays: 14, RemindersEnabled: false, ReminderOffsetDays: []int{2, 9}}, s)
	})

	t.Run("a read failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM receivable_settings", dbtest.ErrRow(errBoom))
		_, err := repo().GetSettings(tx.Context(t.Context()), garageID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("saves", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectExec("INSERT INTO receivable_settings", dbtest.Tag(1), nil).WithArgs(func(args []any) {
			require.Equal(t, []any{garageID, 14, true, []int32{2, 9}}, args)
		})
		require.NoError(t, repo().SaveSettings(tx.Context(t.Context()), garageID, domain.Settings{
			PaymentTermsDays: 14, RemindersEnabled: true, ReminderOffsetDays: []int{2, 9},
		}))

		tx = dbtest.New(t).ExpectExec("INSERT INTO receivable_settings", dbtest.Tag(0), errBoom)
		require.ErrorIs(t, repo().SaveSettings(tx.Context(t.Context()), garageID, domain.DefaultSettings()), errBoom)
	})
}

func TestFind(t *testing.T) {
	t.Parallel()

	t.Run("by settlement", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("settlement_id = $2", dbtest.Row(receivableRow()...))
		r, err := repo().FindBySettlement(tx.Context(t.Context()), garageID, settlementID)
		require.NoError(t, err)
		require.Equal(t, "c-1", r.CustomerID)
		require.Equal(t, domain.StatusOpen, r.Status)
		require.Equal(t, due, r.DueDate)
		require.Equal(t, 1, r.RemindersSent)

		tx = dbtest.New(t).ExpectQueryRow("settlement_id = $2", dbtest.ErrRow(pgxNoRows()))
		_, err = repo().FindBySettlement(tx.Context(t.Context()), garageID, settlementID)
		require.ErrorIs(t, err, domain.ErrReceivableNotFound)

		tx = dbtest.New(t).ExpectQueryRow("settlement_id = $2", dbtest.ErrRow(errBoom))
		_, err = repo().FindBySettlement(tx.Context(t.Context()), garageID, settlementID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("a receivable with no customer", func(t *testing.T) {
		t.Parallel()

		row := receivableRow()
		row[4] = nil
		tx := dbtest.New(t).ExpectQueryRow("settlement_id = $2", dbtest.Row(row...))
		r, err := repo().FindBySettlement(tx.Context(t.Context()), garageID, settlementID)
		require.NoError(t, err)
		require.Empty(t, r.CustomerID)
	})

	t.Run("by id, with its ledger and reminders", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.Row(receivableRow()...)).
			ExpectQuery("FROM receivable_entries", dbtest.NewRows(
				[]any{"e-1", "charge", int64(1_000_000), "QT-0001", nil, "charge:s-1", now},
				[]any{"e-2", "payment", int64(400_000), "PT-1", new("cash"), "payment:p-1", now},
			), nil).
			ExpectQuery("FROM receivable_reminders", dbtest.NewRows([]any{"m-1", 3, "email", "skipped", "NO_EMAIL", 1, now, now}), nil)

		r, err := repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.NoError(t, err)
		require.Len(t, r.Entries, 2)
		require.Equal(t, domain.EntryPayment, r.Entries[1].Kind)
		require.Equal(t, "cash", r.Entries[1].Method)
		require.Empty(t, r.Entries[0].Method)
		require.Equal(t, []domain.Reminder{{
			ID: "m-1", OffsetDays: 3, Channel: "email", Outcome: domain.OutcomeSkipped, Reason: "NO_EMAIL", Attempts: 1, CreatedAt: now, UpdatedAt: now,
		}}, r.Reminders)
	})

	t.Run("by id: not found and each failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.ErrRow(pgxNoRows()))
		_, err := repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, domain.ErrReceivableNotFound)

		tx = dbtest.New(t).ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.ErrRow(errBoom))
		_, err = repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).
			ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.Row(receivableRow()...)).
			ExpectQuery("FROM receivable_entries", nil, errBoom)
		_, err = repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).
			ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.Row(receivableRow()...)).
			ExpectQuery("FROM receivable_entries", dbtest.NewRows([]any{"e-1"}).WithScanError(errBoom), nil)
		_, err = repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).
			ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.Row(receivableRow()...)).
			ExpectQuery("FROM receivable_entries", dbtest.NewRows(), nil).
			ExpectQuery("FROM receivable_reminders", nil, errBoom)
		_, err = repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).
			ExpectQueryRow("FROM receivables WHERE garage_id = $1 AND id = $2", dbtest.Row(receivableRow()...)).
			ExpectQuery("FROM receivable_entries", dbtest.NewRows(), nil).
			ExpectQuery("FROM receivable_reminders", dbtest.NewRows([]any{"m-1"}).WithScanError(errBoom), nil)
		_, err = repo().FindByID(tx.Context(t.Context()), garageID, receivableID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestSaveAndEntries(t *testing.T) {
	t.Parallel()

	r := &domain.Receivable{
		GarageID: garageID, SettlementID: settlementID, SettlementNumber: "QT-0001", CustomerID: "c-1", CustomerName: "An",
		VehicleLicensePlate: "51A", TotalAmount: 10, PaidAmount: 4, DueDate: due, Status: domain.StatusOpen,
	}

	tx := dbtest.New(t).ExpectQueryRow("INSERT INTO receivables", dbtest.Row(receivableID, now, now))
	require.NoError(t, repo().Save(tx.Context(t.Context()), r))
	require.Equal(t, receivableID, r.ID)

	r.CustomerID = ""
	tx = dbtest.New(t).ExpectQueryRow("INSERT INTO receivables", dbtest.ErrRow(errBoom)).WithArgs(func(args []any) {
		require.Nil(t, args[3], "no customer is a NULL")
	})
	require.ErrorIs(t, repo().Save(tx.Context(t.Context()), r), errBoom)

	entries := []domain.Entry{
		{Kind: domain.EntryCharge, Amount: 10, Reference: "QT", SourceKey: "charge:s-1", OccurredAt: now},
		{Kind: domain.EntryPayment, Amount: 4, Reference: "PT", Method: "cash", SourceKey: "payment:p-1", OccurredAt: now},
	}
	tx = dbtest.New(t).
		ExpectExec("INSERT INTO receivable_entries", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO receivable_entries", dbtest.Tag(0), nil)
	require.NoError(t, repo().AddEntries(tx.Context(t.Context()), garageID, receivableID, entries))

	tx = dbtest.New(t).ExpectExec("INSERT INTO receivable_entries", dbtest.Tag(0), errBoom)
	require.ErrorIs(t, repo().AddEntries(tx.Context(t.Context()), garageID, receivableID, entries), errBoom)
}

func TestList(t *testing.T) {
	t.Parallel()

	filter := usecase.ListFilter{CustomerID: "c-1", OverdueOnly: true, Query: "50%_off", Today: due, Limit: 20}
	summary := dbtest.Row(3, 2, int64(1_200_000), 1, int64(600_000))

	t.Run("a page and the summary of the whole match", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("count(*)", summary).WithArgs(func(args []any) {
			require.Equal(t, `50\%\_off`, args[5], "wildcards in the search are matched literally")
		}).
			ExpectQuery("ORDER BY", dbtest.NewRows(receivableRow(), receivableRow()), nil)

		items, total, sum, err := repo().List(tx.Context(t.Context()), garageID, filter)
		require.NoError(t, err)
		require.Equal(t, 3, total)
		require.Len(t, items, 2)
		require.Equal(t, usecase.Summary{OpenCount: 2, OutstandingAmount: 1_200_000, OverdueCount: 1, OverdueAmount: 600_000}, sum)
	})

	t.Run("no match skips the page query", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("count(*)", dbtest.Row(0, 0, int64(0), 0, int64(0)))
		items, total, _, err := repo().List(tx.Context(t.Context()), garageID, filter)
		require.NoError(t, err)
		require.Zero(t, total)
		require.Empty(t, items)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("count(*)", dbtest.ErrRow(errBoom))
		_, _, _, err := repo().List(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", summary).ExpectQuery("ORDER BY", nil, errBoom)
		_, _, _, err = repo().List(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", summary).ExpectQuery("ORDER BY", dbtest.NewRows([]any{"r"}).WithScanError(errBoom), nil)
		_, _, _, err = repo().List(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", summary).ExpectQuery("ORDER BY", dbtest.NewRows(receivableRow()).WithErr(errBoom), nil)
		_, _, _, err = repo().List(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestReminderQueries(t *testing.T) {
	t.Parallel()

	t.Run("garages with something to remind", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQuery("receivable_reminder_garages", dbtest.NewRows([]any{"g-1"}, []any{"g-2"}), nil)
		ids, err := repo().ReminderGarages(tx.Context(t.Context()))
		require.NoError(t, err)
		require.Equal(t, []string{"g-1", "g-2"}, ids)

		tx = dbtest.New(t).ExpectQuery("receivable_reminder_garages", nil, errBoom)
		_, err = repo().ReminderGarages(tx.Context(t.Context()))
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQuery("receivable_reminder_garages", dbtest.NewRows([]any{"g"}).WithScanError(errBoom), nil)
		_, err = repo().ReminderGarages(tx.Context(t.Context()))
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("open receivables with their reminders", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("customer_id IS NOT NULL", dbtest.NewRows(receivableRow()), nil).
			ExpectQuery("FROM receivable_reminders", dbtest.NewRows([]any{"m-1", 3, "email", "sent", nil, 1, now, now}), nil)
		items, err := repo().OpenWithCustomer(tx.Context(t.Context()), garageID)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, domain.OutcomeSent, items[0].Reminders[0].Outcome)
	})

	t.Run("open receivables: failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQuery("customer_id IS NOT NULL", nil, errBoom)
		_, err := repo().OpenWithCustomer(tx.Context(t.Context()), garageID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQuery("customer_id IS NOT NULL", dbtest.NewRows([]any{"r"}).WithScanError(errBoom), nil)
		_, err = repo().OpenWithCustomer(tx.Context(t.Context()), garageID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQuery("customer_id IS NOT NULL", dbtest.NewRows(receivableRow()).WithErr(errBoom), nil)
		_, err = repo().OpenWithCustomer(tx.Context(t.Context()), garageID)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).
			ExpectQuery("customer_id IS NOT NULL", dbtest.NewRows(receivableRow()), nil).
			ExpectQuery("FROM receivable_reminders", nil, errBoom)
		_, err = repo().OpenWithCustomer(tx.Context(t.Context()), garageID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("lock, log and count", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("pg_try_advisory_xact_lock", dbtest.Row(true))
		ok, err := repo().TryLock(tx.Context(t.Context()), "k")
		require.NoError(t, err)
		require.True(t, ok)

		tx = dbtest.New(t).ExpectQueryRow("pg_try_advisory_xact_lock", dbtest.ErrRow(errBoom))
		_, err = repo().TryLock(tx.Context(t.Context()), "k")
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectExec("INSERT INTO receivable_reminders", dbtest.Tag(1), nil).WithArgs(func(args []any) {
			require.Nil(t, args[5], "no reason is a NULL")
		})
		require.NoError(t, repo().SaveReminder(tx.Context(t.Context()), garageID, receivableID,
			&domain.Reminder{OffsetDays: 3, Channel: "email", Outcome: domain.OutcomeSent, Attempts: 1}))

		tx = dbtest.New(t).ExpectExec("INSERT INTO receivable_reminders", dbtest.Tag(0), errBoom)
		require.ErrorIs(t, repo().SaveReminder(tx.Context(t.Context()), garageID, receivableID, &domain.Reminder{}), errBoom)

		tx = dbtest.New(t).ExpectExec("UPDATE receivables SET reminders_sent", dbtest.Tag(1), nil)
		require.NoError(t, repo().MarkReminded(tx.Context(t.Context()), garageID, receivableID, now))

		tx = dbtest.New(t).ExpectExec("UPDATE receivables SET reminders_sent", dbtest.Tag(0), errBoom)
		require.ErrorIs(t, repo().MarkReminded(tx.Context(t.Context()), garageID, receivableID, now), errBoom)
	})
}

func TestLedgerQueries(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func(ctx contextT, r *repository.PostgresRepository) (int64, error){
		"balance before": func(ctx contextT, r *repository.PostgresRepository) (int64, error) {
			return r.BalanceBefore(ctx, garageID, now)
		},
		"ledger balance": func(ctx contextT, r *repository.PostgresRepository) (int64, error) {
			return r.LedgerBalance(ctx, garageID)
		},
		"remaining": func(ctx contextT, r *repository.PostgresRepository) (int64, error) {
			return r.RemainingTotal(ctx, garageID)
		},
	} {
		tx := dbtest.New(t).ExpectQueryRow("SELECT COALESCE", dbtest.Row(int64(42)))
		got, err := call(tx.Context(t.Context()), repo())
		require.NoError(t, err, name)
		require.EqualValues(t, 42, got, name)

		tx = dbtest.New(t).ExpectQueryRow("SELECT COALESCE", dbtest.ErrRow(errBoom))
		_, err = call(tx.Context(t.Context()), repo())
		require.ErrorIs(t, err, errBoom, name)
	}

	t.Run("entries of a period", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQuery("FROM receivable_entries e", dbtest.NewRows(
			[]any{"e-1", "charge", int64(10), "QT", nil, "charge:s", now, "An"},
			[]any{"e-2", "payment", int64(4), "PT", new("cash"), "payment:p", now, "An"},
		), nil)
		lines, err := repo().Entries(tx.Context(t.Context()), garageID, now, now.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, lines, 2)
		require.Equal(t, "cash", lines[1].Entry.Method)
		require.Equal(t, "An", lines[0].CustomerName)

		tx = dbtest.New(t).ExpectQuery("FROM receivable_entries e", nil, errBoom)
		_, err = repo().Entries(tx.Context(t.Context()), garageID, now, now)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQuery("FROM receivable_entries e", dbtest.NewRows([]any{"e"}).WithScanError(errBoom), nil)
		_, err = repo().Entries(tx.Context(t.Context()), garageID, now, now)
		require.ErrorIs(t, err, errBoom)
	})
}
