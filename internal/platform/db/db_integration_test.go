//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const txTestTable = "tx_manager_test"

func startPostgres(t *testing.T) (*sql.DB, *dbpkg.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "vietgara_test",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/vietgara_test?sslmode=disable", host, port.Port())

	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	require.NoError(t, admin.PingContext(ctx))

	connPool, err := dbpkg.NewPgxPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(connPool.Close)

	pool := dbpkg.NewPool(ctx, connPool)

	_, err = admin.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+txTestTable+" (id int PRIMARY KEY)")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+txTestTable)
	})

	return admin, pool
}

func TestTxManager_WithTransactionReal(t *testing.T) {
	admin, pool := startPostgres(t)
	m := dbpkg.NewTxManager(pool)

	garageID := "garage-integration-1"

	t.Run("commits and scopes work to tenant", func(t *testing.T) {
		_, err := admin.ExecContext(t.Context(), "TRUNCATE "+txTestTable)
		require.NoError(t, err)

		var gotSetting string

		err = m.WithTransaction(t.Context(), &garageID, func(ctx context.Context) error {
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

		wantErr := errors.New("callback failed")

		err = m.WithTransaction(t.Context(), &garageID, func(ctx context.Context) error {
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

		err := m.WithTransaction(t.Context(), nil, func(ctx context.Context) error {
			return dbpkg.QuerierFromContext(ctx, pool).
				QueryRow(ctx, "SELECT COALESCE(current_setting('app.current_garage_id', true), '')").
				Scan(&gotSetting)
		})
		require.NoError(t, err)
		require.Empty(t, gotSetting)
	})
}
