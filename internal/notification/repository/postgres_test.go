package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/repository"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		log := &domain.Log{
			ID: "n-1", UserID: "user-1", Channel: domain.ChannelEmail, Template: "registration_confirmation",
			Recipient: "an@example.com", Status: domain.StatusSent,
		}

		// An account e-mail has no garage: nothing is read back.
		tx := dbtest.New(t).ExpectExec("INSERT INTO notification_log", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO notification_log", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, log))
		require.ErrorIs(t, repo.Create(ctx, log), errBoom)
	})

	t.Run("a garage row reads back its timestamp", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		log := &domain.Log{ID: "n-1", Channel: domain.ChannelEmail, Template: "t", Recipient: "a@b.com", Status: domain.StatusSent, GarageID: "g-1"}

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO notification_log", dbtest.Row(now)).
			ExpectQueryRow("INSERT INTO notification_log", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, log))
		require.Equal(t, now, log.CreatedAt)
		require.ErrorIs(t, repo.Create(ctx, log), errBoom)
	})
}

func TestPostgresRepository_CreateKeepsTheGarageFields(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	log := &domain.Log{
		ID: "n-1", Channel: domain.ChannelEmail, Template: "vehicle_ready", Recipient: "an@example.com", Status: domain.StatusSkipped,
		GarageID: "g-1", CustomerID: "c-1", Type: domain.TypeVehicleReady, EntityType: "repair_order", EntityID: "ro-1",
		EventID: "e-1", SkipReason: domain.SkipOptedOut,
	}

	tx := dbtest.New(t).ExpectQueryRow("INSERT INTO notification_log", dbtest.Row(now)).WithArgs(func(args []any) {
		require.Len(t, args, 14)
		require.Equal(t, "skipped", args[5])
		require.Equal(t, "g-1", derefString(t, args[7]))
		require.Equal(t, "opted_out", derefString(t, args[13]))
		require.Nil(t, args[1], "no user")
	})

	require.NoError(t, repo.Create(tx.Context(t.Context()), log))
}

type fakeTransactor struct {
	garage string
	ctx    context.Context
}

func (f *fakeTransactor) InGarage(_ context.Context, garageID string, fn func(ctx context.Context) error) error {
	f.garage = garageID

	return fn(f.ctx)
}

func TestPostgresRepository_Failed(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).ExpectQueryRow("INSERT INTO notification_log", dbtest.Row(now))
	transactor := &fakeTransactor{ctx: tx.Context(t.Context())}
	repo := repository.NewPostgresRepository(nil).WithFailureTx(transactor)

	log := &domain.Log{ID: "n-1", Channel: domain.ChannelEmail, Template: "t", Recipient: "a@b.com", Status: domain.StatusFailed, GarageID: "g-1", Error: "boom"}
	require.NoError(t, repo.Failed(t.Context(), log))
	require.Equal(t, "g-1", transactor.garage, "written in the garage's own transaction")
	require.Equal(t, now, log.CreatedAt)
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	filter := usecase.LogFilter{Type: domain.TypeLowStock, Status: domain.StatusSent}

	tx := dbtest.New(t).
		ExpectQueryRow("SELECT count(*) FROM notification_log", dbtest.Row(41)).
		WithArgs(func(args []any) { require.Equal(t, []any{"g-1", "low_stock", "sent"}, args) }).
		ExpectQuery("FROM notification_log", dbtest.NewRows(
			[]any{"n-1", "low_stock", "lan@example.com", "", "part", "p-1", "sent", "", "", now},
			[]any{"n-2", "", "x@example.com", "c-1", "", "", "skipped", "no_email", "", now},
		), nil).
		WithArgs(func(args []any) { require.Equal(t, []any{"g-1", "low_stock", "sent", 20, 40}, args) }).
		ExpectQueryRow("SELECT count(*) FROM notification_log", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("SELECT count(*) FROM notification_log", dbtest.Row(1)).
		ExpectQuery("FROM notification_log", nil, errBoom).
		ExpectQueryRow("SELECT count(*) FROM notification_log", dbtest.Row(1)).
		ExpectQuery("FROM notification_log", dbtest.NewRows([]any{"n-1", "", "", "", "", "", "sent", "", "", now}).WithScanError(errBoom), nil).
		ExpectQueryRow("SELECT count(*) FROM notification_log", dbtest.Row(1)).
		ExpectQuery("FROM notification_log", dbtest.NewRows([]any{"n-1", "", "", "", "", "", "sent", "", "", now}).WithErr(errBoom), nil)
	ctx := tx.Context(t.Context())

	logs, total, err := repo.List(ctx, "g-1", filter, 3, 20)
	require.NoError(t, err)
	require.Equal(t, 41, total)
	require.Len(t, logs, 2)
	require.Equal(t, domain.TypeLowStock, logs[0].Type)
	require.Equal(t, "p-1", logs[0].EntityID)
	require.Equal(t, domain.SkipNoEmail, logs[1].SkipReason)
	require.Equal(t, domain.StatusSkipped, logs[1].Status)
	require.Equal(t, "g-1", logs[0].GarageID)

	for range 4 {
		_, _, err = repo.List(ctx, "g-1", usecase.LogFilter{}, 1, 20)
		require.ErrorIs(t, err, errBoom)
	}
}

// derefString reads the string a nullable argument points to.
func derefString(t *testing.T, arg any) string {
	t.Helper()

	p, ok := arg.(*string)
	require.True(t, ok, "a *string argument")
	require.NotNil(t, p)

	return *p
}

// bytesOf reads a []byte argument.
func bytesOf(t *testing.T, arg any) []byte {
	t.Helper()

	b, ok := arg.([]byte)
	require.True(t, ok, "a []byte argument")

	return b
}
