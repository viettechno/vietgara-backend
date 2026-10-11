//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

const txTestTable = "tx_manager_test"

func startPostgres(t *testing.T) (*sql.DB, *dbpkg.Pool) {
	t.Helper()

	pg := pgtest.Start(t)

	_, err := pg.Admin.ExecContext(t.Context(), "CREATE TABLE IF NOT EXISTS "+txTestTable+" (id int PRIMARY KEY)")
	require.NoError(t, err)

	return pg.Admin, pg.SuperPool(t)
}

func TestTxManager_WithTransactionReal(t *testing.T) {
	admin, pool := startPostgres(t)
	m := dbpkg.NewTxManager(pool)

	garageID := "garage-integration-1"

	t.Run("commits and scopes work to tenant", func(t *testing.T) {
		_, err := admin.ExecContext(t.Context(), "TRUNCATE "+txTestTable)
		require.NoError(t, err)

		var gotSetting string

		err = m.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
			querier := dbpkg.QuerierFromContext(ctx, pool)

			err := querier.QueryRow(ctx, "SELECT current_setting('app.current_garage_id')").Scan(&gotSetting)
			if err != nil {
				return err
			}

			_, err = querier.Exec(ctx, "INSERT INTO "+txTestTable+" (id) VALUES (1)")

			return err
		})
		require.NoError(t, err)
		require.Equal(t, garageID, gotSetting)

		var count int
		require.NoError(t, admin.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+txTestTable).Scan(&count))
		require.Equal(t, 1, count)

		var sessionSetting string
		require.NoError(t, admin.QueryRowContext(t.Context(),
			"SELECT COALESCE(current_setting('app.current_garage_id', true), '')").Scan(&sessionSetting))
		require.Empty(t, sessionSetting)
	})

	t.Run("rolls back when callback fails", func(t *testing.T) {
		_, err := admin.ExecContext(t.Context(), "TRUNCATE "+txTestTable)
		require.NoError(t, err)

		wantErr := errIntegrationCallbackFailed

		err = m.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
			_, err := dbpkg.QuerierFromContext(ctx, pool).Exec(ctx, "INSERT INTO "+txTestTable+" (id) VALUES (2)")
			if err != nil {
				return err
			}

			return wantErr
		})
		require.ErrorIs(t, err, wantErr)

		var count int
		require.NoError(t, admin.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+txTestTable).Scan(&count))
		require.Equal(t, 0, count)
	})

	t.Run("skips tenant variable when garageID is nil", func(t *testing.T) {
		var gotSetting string

		err := m.WithTransaction(t.Context(), dbpkg.Scope{}, func(ctx context.Context) error {
			return dbpkg.QuerierFromContext(ctx, pool).
				QueryRow(ctx, "SELECT COALESCE(current_setting('app.current_garage_id', true), '')").
				Scan(&gotSetting)
		})
		require.NoError(t, err)
		require.Empty(t, gotSetting)
	})
}
