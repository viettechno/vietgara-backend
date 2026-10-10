// Package pgtest starts a disposable PostgreSQL for integration tests: one
// testcontainers instance per call, migrated with the same golang-migrate
// path the server runs on startup, plus pools bound to the superuser (which
// bypasses RLS) or to a vietgara_app login (which every FORCE ROW LEVEL
// SECURITY policy applies to, like the running backend).
//
//	pg := pgtest.StartMigrated(tb)
//	pool := pg.AppPool(tb)
//	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users ...`) // seed past RLS
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/lib/pq" // database/sql driver for Admin.
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/viettechno/vietgara-backend/db"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Image is the PostgreSQL image every integration test runs on. Keep it in
// step with vietgara-pgsql in docker-compose.yml and the Cloud SQL major
// version, so RLS and planner behaviour match what the backend runs on.
const Image = "postgres:18.4"

const (
	superUser     = "postgres"
	superPassword = "postgres"
	database      = "vietgara_test"

	startupTimeout = 2 * time.Minute
)

// The vietgara_app login AppPool connects as.
const (
	// AppUser is the login name.
	AppUser = "app_rls"
	// AppPassword is its password.
	AppPassword = "appsecret"
)

// Postgres is a running, empty or migrated, database.
type Postgres struct {
	// Admin is a superuser connection: it bypasses RLS, so tests seed and
	// inspect rows through it.
	Admin *sql.DB

	host string
	port string
}

// Start boots an empty PostgreSQL that is terminated when t ends.
func Start(tb testing.TB) *Postgres {
	tb.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	tb.Cleanup(cancel)

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        Image,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       database,
				"POSTGRES_USER":     superUser,
				"POSTGRES_PASSWORD": superPassword,
			},
			// The entrypoint starts a temporary server for initdb first; the
			// second "ready" line is the real one.
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(startupTimeout),
		},
		Started: true,
	})
	if container != nil {
		tb.Cleanup(func() { _ = container.Terminate(context.Background()) })
	}

	if err != nil {
		tb.Fatalf("pgtest: start %s: %v", Image, err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		tb.Fatalf("pgtest: container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		tb.Fatalf("pgtest: mapped port: %v", err)
	}

	pg := &Postgres{host: host, port: port.Port()}

	pg.Admin, err = sql.Open("postgres", pg.SuperDSN())
	if err != nil {
		tb.Fatalf("pgtest: open admin connection: %v", err)
	}

	tb.Cleanup(func() { _ = pg.Admin.Close() })

	if err := pg.Admin.PingContext(ctx); err != nil {
		tb.Fatalf("pgtest: ping: %v", err)
	}

	return pg
}

// StartMigrated boots PostgreSQL and applies every migration in
// db/migrations.
func StartMigrated(tb testing.TB) *Postgres {
	tb.Helper()

	pg := Start(tb)
	if err := db.RunMigrations(pg.SuperDSN(), MigrationsURL()); err != nil {
		tb.Fatalf("pgtest: %v", err)
	}

	return pg
}

// DSN returns a connection string for user.
func (pg *Postgres) DSN(user, password string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, pg.host, pg.port, database)
}

// SuperDSN returns the superuser connection string.
func (pg *Postgres) SuperDSN() string { return pg.DSN(superUser, superPassword) }

// SuperPool returns a pool bound to the superuser, which bypasses RLS.
func (pg *Postgres) SuperPool(tb testing.TB) *platformdb.Pool {
	tb.Helper()

	return pg.pool(tb, pg.SuperDSN())
}

// AppPool creates the AppUser login in vietgara_app the way the server does
// on startup (db.EnsureAppLogin) and returns a pool bound to it, so RLS
// applies. The database must be migrated: the role and its grants come from
// the migrations.
func (pg *Postgres) AppPool(tb testing.TB) *platformdb.Pool {
	tb.Helper()

	if err := db.EnsureAppLogin(pg.SuperDSN(), AppUser, AppPassword); err != nil {
		tb.Fatalf("pgtest: %v", err)
	}

	return pg.pool(tb, pg.DSN(AppUser, AppPassword))
}

func (pg *Postgres) pool(tb testing.TB, dsn string) *platformdb.Pool {
	tb.Helper()

	conn, err := platformdb.NewPgxPool(context.Background(), dsn)
	if err != nil {
		tb.Fatalf("pgtest: %v", err)
	}

	tb.Cleanup(conn.Close)

	return platformdb.NewPool(context.Background(), conn)
}

// MigrationsURL returns the golang-migrate source URL of db/migrations,
// resolved from this file so it works from any test package.
func MigrationsURL() string {
	_, file, _, _ := runtime.Caller(0)

	return "file://" + filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "db", "migrations")
}
