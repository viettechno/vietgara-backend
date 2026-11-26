//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/repository"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestPrinterStore runs the printers as the RLS-bound application role: the
// names are unique without regard to case, the rows of one garage are
// invisible to another, and a roll print is logged with its roll.
func TestPrinterStore(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	store := repository.NewPostgresPrinterStore(pool)
	prints := repository.NewPostgresPrintStore(pool)
	txm := platformdb.NewTxManager(pool)

	userID, garageA, garageB := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	require.NoError(t, err)

	for _, id := range []string{garageA, garageB} {
		_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, $3)`, id, userID, "garage-"+id[:8])
		require.NoError(t, err)
	}

	in := func(garageID string, fn func(ctx context.Context)) {
		t.Helper()
		require.NoError(t, txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageID, UserID: userID}, func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
	}

	var counter usecase.Printer

	in(garageA, func(ctx context.Context) {
		require.NoError(t, store.LockPrinters(ctx, garageA))

		counter, err = store.CreatePrinter(ctx, garageA, usecase.Printer{
			Name: "Quầy", Protocol: usecase.ProtocolRaw, Host: "192.168.1.50", Port: 9100, Paper: printmodel.K80,
			DocumentTypes: []printmodel.DocumentType{printmodel.TypeQuote, printmodel.TypePaymentVoucher},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, counter.ID)
		assert.False(t, counter.CreatedAt.IsZero())

		office, err := store.CreatePrinter(ctx, garageA, usecase.Printer{
			Name: "Văn phòng", Protocol: usecase.ProtocolIPP, Host: "printer.local", Port: 631, Path: "ipp/print",
		})
		require.NoError(t, err)
		assert.Empty(t, office.Paper)
		assert.Empty(t, office.DocumentTypes)
	})

	err = txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageA, UserID: userID}, func(ctx context.Context) error {
		_, err := store.CreatePrinter(ctx, garageA, usecase.Printer{Name: "QUẦY", Protocol: usecase.ProtocolRaw, Host: "h", Port: 9100, Paper: printmodel.K57})

		return err
	})
	require.ErrorIs(t, err, usecase.ErrPrinterNameTaken, "names are unique without regard to case")

	in(garageA, func(ctx context.Context) {
		n, err := store.CountPrinters(ctx, garageA)
		require.NoError(t, err)
		assert.Equal(t, 2, n)

		list, err := store.ListPrinters(ctx, garageA)
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, "Quầy", list[0].Name)

		got, err := store.GetPrinter(ctx, garageA, counter.ID)
		require.NoError(t, err)
		assert.Equal(t, []printmodel.DocumentType{printmodel.TypeQuote, printmodel.TypePaymentVoucher}, got.DocumentTypes)

		counter.Paper, counter.DocumentTypes = printmodel.Label5030, []printmodel.DocumentType{printmodel.TypePartLabel}
		updated, err := store.UpdatePrinter(ctx, garageA, counter)
		require.NoError(t, err)
		assert.Equal(t, printmodel.Label5030, updated.Paper)

		_, err = store.GetPrinter(ctx, garageA, "not-a-uuid")
		require.ErrorIs(t, err, usecase.ErrPrinterNotFound)

		require.NoError(t, prints.Record(ctx, garageA, printmodel.TypePaymentVoucher, uuid.NewString(), printmodel.K57, 1, false), "the log takes the rolls")
	})

	in(garageB, func(ctx context.Context) {
		list, err := store.ListPrinters(ctx, garageB)
		require.NoError(t, err)
		assert.Empty(t, list, "another garage sees none of garage A's printers")

		_, err = store.GetPrinter(ctx, garageB, counter.ID)
		require.ErrorIs(t, err, usecase.ErrPrinterNotFound)

		_, err = store.UpdatePrinter(ctx, garageB, counter)
		require.ErrorIs(t, err, usecase.ErrPrinterNotFound)

		require.ErrorIs(t, store.DeletePrinter(ctx, garageB, counter.ID), usecase.ErrPrinterNotFound)
	})

	in(garageA, func(ctx context.Context) {
		require.NoError(t, store.DeletePrinter(ctx, garageA, counter.ID))
		require.ErrorIs(t, store.DeletePrinter(ctx, garageA, counter.ID), usecase.ErrPrinterNotFound)
		require.ErrorIs(t, store.DeletePrinter(ctx, garageA, "not-a-uuid"), usecase.ErrPrinterNotFound)
	})

	err = txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageB, UserID: userID}, func(ctx context.Context) error {
		_, err := store.CreatePrinter(ctx, garageA, usecase.Printer{Name: "X", Protocol: usecase.ProtocolIPP, Host: "h", Port: 631, Path: "p"})

		return err
	})
	require.Error(t, err, "row-level security refuses a row of another garage")
}
