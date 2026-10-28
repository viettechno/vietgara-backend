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

// TestPrintStore runs the print settings and the print log as the RLS-bound
// application role: one garage's rows are invisible to another, the earliest
// print of a document pins its version, and the log cannot be rewritten.
func TestPrintStore(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	store := repository.NewPostgresPrintStore(pool)
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

	doc := uuid.NewString()

	in(garageA, func(ctx context.Context) {
		settings, err := store.Settings(ctx, garageA)
		require.NoError(t, err)
		assert.Empty(t, settings, "a garage that never chose has no rows")

		require.NoError(t, store.SaveSetting(ctx, garageA, printmodel.TypeQuote, usecase.PrintSetting{Paper: printmodel.A5, CopyMark: true}))
		require.NoError(t, store.SaveSetting(ctx, garageA, printmodel.TypeQuote, usecase.PrintSetting{Paper: printmodel.A4}), "a later save replaces")

		settings, err = store.Settings(ctx, garageA)
		require.NoError(t, err)
		assert.Equal(t, map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A4}}, settings)

		state, err := store.State(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A4)
		require.NoError(t, err)
		assert.Equal(t, usecase.PrintState{}, state, "never printed")

		require.NoError(t, store.Record(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A4, 1, false))
		require.NoError(t, store.Record(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A5, 2, true))
		require.NoError(t, store.Record(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A4, 2, true))

		state, err = store.State(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A4)
		require.NoError(t, err)
		assert.Equal(t, usecase.PrintState{FirstVersion: 1, Prints: 3}, state, "the earliest print on A4 pins the version; every paper counts")

		state, err = store.State(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A5)
		require.NoError(t, err)
		assert.Equal(t, usecase.PrintState{FirstVersion: 2, Prints: 3}, state)
	})

	in(garageB, func(ctx context.Context) {
		settings, err := store.Settings(ctx, garageB)
		require.NoError(t, err)
		assert.Empty(t, settings, "another garage sees none of garage A's settings")

		state, err := store.State(ctx, garageB, printmodel.TypeQuote, doc, printmodel.A4)
		require.NoError(t, err)
		assert.Equal(t, usecase.PrintState{}, state, "nor its prints")
	})

	err = txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageB, UserID: userID}, func(ctx context.Context) error {
		return store.Record(ctx, garageA, printmodel.TypeQuote, doc, printmodel.A4, 1, false)
	})
	require.Error(t, err, "garage B cannot write a row of garage A")

	var who string

	require.NoError(t, pg.Admin.QueryRowContext(ctx, `SELECT created_by::text FROM print_log WHERE garage_id = $1 LIMIT 1`, garageA).Scan(&who))
	assert.Equal(t, userID, who, "the audit trigger records who printed")

	for _, statement := range []string{
		`UPDATE print_log SET copy = NOT copy WHERE garage_id = $1`,
		`DELETE FROM print_log WHERE garage_id = $1`,
	} {
		err = txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageA, UserID: userID}, func(ctx context.Context) error {
			_, execErr := platformdb.QuerierFromContext(ctx, pool).Exec(ctx, statement, garageA)

			return execErr
		})
		require.Error(t, err, "the log is append-only: %s", statement)
	}
}
