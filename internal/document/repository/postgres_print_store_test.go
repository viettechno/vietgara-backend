package repository_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/repository"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var errBoom = errors.New("boom")

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	documentID = "22222222-2222-2222-2222-222222222222"
)

func TestSettings(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresPrintStore(nil)

	tx := dbtest.New(t).
		ExpectQuery("FROM garage_print_settings", dbtest.NewRows([]any{"quote", "A5", true}, []any{"settlement", "A4", false}), nil).
		WithArgs(func(args []any) { assert.Equal(t, []any{garageID}, args) }).
		ExpectQuery("FROM garage_print_settings", nil, errBoom).
		ExpectQuery("FROM garage_print_settings", dbtest.NewRows([]any{"quote", "A5", true}).WithScanError(errBoom), nil).
		ExpectQuery("FROM garage_print_settings", dbtest.NewRows([]any{"quote", "A5", true}).WithErr(errBoom), nil)
	ctx := tx.Context(t.Context())

	settings, err := store.Settings(ctx, garageID)
	require.NoError(t, err)
	assert.Equal(t, map[printmodel.DocumentType]usecase.PrintSetting{
		printmodel.TypeQuote:      {Paper: printmodel.A5, CopyMark: true},
		printmodel.TypeSettlement: {Paper: printmodel.A4},
	}, settings)

	for range 3 {
		_, err = store.Settings(ctx, garageID)
		require.ErrorIs(t, err, errBoom)
	}
}

func TestSaveSetting(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresPrintStore(nil)

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO garage_print_settings", dbtest.Tag(1), nil).
		WithArgs(func(args []any) { assert.Equal(t, []any{garageID, "payment_voucher", "A5", true}, args) }).
		ExpectExec("INSERT INTO garage_print_settings", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, store.SaveSetting(ctx, garageID, printmodel.TypePaymentVoucher, usecase.PrintSetting{Paper: printmodel.A5, CopyMark: true}))
	require.ErrorIs(t, store.SaveSetting(ctx, garageID, printmodel.TypeQuote, usecase.DefaultPrintSetting), errBoom)
}

func TestState(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresPrintStore(nil)

	tx := dbtest.New(t).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(1), nil).
		ExpectQueryRow("FROM print_log", dbtest.Row(1, 3)).
		WithArgs(func(args []any) { assert.Equal(t, []any{garageID, "quote", documentID, "A4"}, args) }).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(1), nil).
		ExpectQueryRow("FROM print_log", dbtest.ErrRow(errBoom)).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	state, err := store.State(ctx, garageID, printmodel.TypeQuote, documentID, printmodel.A4)
	require.NoError(t, err)
	assert.Equal(t, usecase.PrintState{FirstVersion: 1, Prints: 3}, state)

	_, err = store.State(ctx, garageID, printmodel.TypeQuote, documentID, printmodel.A4)
	require.ErrorIs(t, err, errBoom)

	_, err = store.State(ctx, garageID, printmodel.TypeQuote, documentID, printmodel.A4)
	require.ErrorIs(t, err, errBoom, "the lock failed")
}

func TestRecord(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresPrintStore(nil)

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO print_log", dbtest.Tag(1), nil).
		WithArgs(func(args []any) { assert.Equal(t, []any{garageID, "quote", documentID, "A5", 1, true}, args) }).
		ExpectExec("INSERT INTO print_log", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, store.Record(ctx, garageID, printmodel.TypeQuote, documentID, printmodel.A5, 1, true))
	require.ErrorIs(t, store.Record(ctx, garageID, printmodel.TypeQuote, documentID, printmodel.A5, 1, true), errBoom)
}
