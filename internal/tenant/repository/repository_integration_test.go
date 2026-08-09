//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

const (
	appRoleName = "app_rls"
	appRolePass = "appsecret"
)

type testEnv struct {
	admin     *sql.DB
	superPool *dbpkg.Pool
	appPool   *dbpkg.Pool
	accountID string
	garageA   string
	garageB   string
}

// startPostgres boots a disposable Postgres, applies the real migrations
// from db/migrations, seeds an account with two garages (plus their owner
// staff records), and returns two pools: one bound to the superuser (which
// bypasses RLS) and one bound to a plain LOGIN role (which is subject to
// the FORCE ROW LEVEL SECURITY policies).
func startPostgres(t *testing.T) *testEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "vietgara_tenant_test",
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

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/vietgara_tenant_test?sslmode=disable", host, port.Port())

	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(ctx))

	applyMigrations(t, ctx, admin)
	createAppRole(t, ctx, admin)

	env := &testEnv{admin: admin}
	env.accountID, env.garageA, env.garageB = seedGarages(t, ctx, admin)

	superConn, err := dbpkg.NewPgxPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(superConn.Close)

	appDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/vietgara_tenant_test?sslmode=disable",
		appRoleName, appRolePass, host, port.Port())
	appConn, err := dbpkg.NewPgxPool(ctx, appDSN)
	require.NoError(t, err)
	t.Cleanup(appConn.Close)

	env.superPool = dbpkg.NewPool(ctx, superConn)
	env.appPool = dbpkg.NewPool(ctx, appConn)

	return env
}

func applyMigrations(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()

	dir := filepath.Join("..", "..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var up []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			up = append(up, entry.Name())
		}
	}
	sort.Strings(up)

	for _, name := range up {
		content, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)

		_, err = admin.ExecContext(ctx, string(content))
		require.NoError(t, err, "applying migration %s", name)
	}
}

func createAppRole(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()

	stmts := []string{
		`CREATE ROLE ` + appRoleName + ` LOGIN PASSWORD '` + appRolePass + `'`,
		"GRANT USAGE ON SCHEMA public TO " + appRoleName,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + appRoleName,
		"GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO " + appRoleName,
	}
	for _, stmt := range stmts {
		_, err := admin.ExecContext(ctx, stmt)
		require.NoError(t, err, "executing %s", stmt)
	}
}

func seedGarages(t *testing.T, ctx context.Context, admin *sql.DB) (accountID, garageA, garageB string) {
	t.Helper()

	accountID = uuid.NewString()
	_, err := admin.ExecContext(ctx,
		`INSERT INTO accounts (id, auth_provider, identity_platform_uid, full_name)
		 VALUES ($1, 'test', $2, 'Test Account')`,
		accountID, "uid-"+accountID,
	)
	require.NoError(t, err)

	garageA, garageB = uuid.NewString(), uuid.NewString()
	for _, id := range []string{garageA, garageB} {
		_, err = admin.ExecContext(ctx,
			`INSERT INTO garages (id, account_id, name) VALUES ($1, $2, $3)`,
			id, accountID, "garage-"+id[:8],
		)
		require.NoError(t, err)
	}

	_, err = admin.ExecContext(ctx,
		`INSERT INTO garage_staff (garage_id, account_id, role) VALUES ($1, $2, 'owner'), ($3, $2, 'owner')`,
		garageA, accountID, garageB,
	)
	require.NoError(t, err)

	return accountID, garageA, garageB
}

// ---------------------------------------------------------------------------
// Superuser path (simulates a BYPASSRLS production role): RLS policies do
// not apply, so all rows are visible regardless of tenant context.
// ---------------------------------------------------------------------------

func TestPostgresGarageRepository_CRUD(t *testing.T) {
	env := startPostgres(t)
	repo := repository.NewPostgresGarageRepository(env.superPool)
	ctx := t.Context()

	t.Run("create, find, update, list round trip", func(t *testing.T) {
		created := &domain.Garage{
			ID:        uuid.NewString(),
			AccountID: env.accountID,
			Name:      "Gara Alpha",
			Timezone:  "Asia/Ho_Chi_Minh",
			Currency:  "VND",
			Status:    domain.GarageStatusActive,
		}
		require.NoError(t, repo.Create(ctx, created))
		require.NotZero(t, created.CreatedAt)
		require.NotZero(t, created.UpdatedAt)

		got, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, created.ID, got.ID)
		require.Equal(t, "Gara Alpha", got.Name)
		require.Equal(t, "Asia/Ho_Chi_Minh", got.Timezone)
		require.Equal(t, domain.GarageStatusActive, got.Status)
		require.Nil(t, got.TaxCode)

		got.Name = "Gara Alpha Renamed"
		require.NoError(t, repo.Update(ctx, got))

		got, err = repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, "Gara Alpha Renamed", got.Name)

		list, err := repo.ListByAccountID(ctx, env.accountID)
		require.NoError(t, err)
		require.Len(t, list, 3)
	})

	t.Run("missing garage maps to ErrGarageNotFound", func(t *testing.T) {
		_, err := repo.FindByID(ctx, uuid.NewString())
		require.ErrorIs(t, err, domain.ErrGarageNotFound)
	})
}

func TestPostgresGarageStaffRepository_CRUD(t *testing.T) {
	env := startPostgres(t)

	repo := repository.NewPostgresGarageStaffRepository(env.superPool)
	ctx := t.Context()
	techID := uuid.NewString()
	techAccountID := uuid.NewString()

	t.Run("create, list, find, and list roles", func(t *testing.T) {
		_, err := env.admin.ExecContext(ctx,
			`INSERT INTO accounts (id, auth_provider, identity_platform_uid, full_name)
			 VALUES ($1, 'test', $2, 'Technician')`,
			techAccountID, "uid-tech-"+techAccountID,
		)
		require.NoError(t, err)

		tech := &domain.GarageStaff{
			ID:        techID,
			GarageID:  env.garageA,
			AccountID: techAccountID,
			Role:      domain.RoleTechnician,
			Status:    domain.StaffStatusActive,
		}
		require.NoError(t, repo.Create(ctx, tech))
		require.NotZero(t, tech.CreatedAt)

		list, err := repo.ListByGarageID(ctx, env.garageA)
		require.NoError(t, err)
		require.Len(t, list, 2)

		got, err := repo.FindByGarageAndAccount(ctx, env.garageA, tech.AccountID)
		require.NoError(t, err)
		require.Equal(t, domain.RoleTechnician, got.Role)

		roles, err := repo.ListRolesForAccount(ctx, tech.AccountID)
		require.NoError(t, err)
		require.Equal(t, []repository.RoleAssignment{{GarageID: env.garageA, Role: "technician"}}, roles)
	})

	t.Run("missing membership returns pgx.ErrNoRows untouched", func(t *testing.T) {
		_, err := repo.FindByGarageAndAccount(ctx, env.garageA, uuid.NewString())
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})
}

// ---------------------------------------------------------------------------
// RLS path (plain login role): the FORCE ROW LEVEL SECURITY policies on
// garages/garage_staff only admit rows for the CURRENT app.current_garage_id
// session variable, which repository code relies on the TxManager to set.
// ---------------------------------------------------------------------------

func TestRepositories_RLSIsolation(t *testing.T) {
	env := startPostgres(t)

	garages := repository.NewPostgresGarageRepository(env.appPool)
	staff := repository.NewPostgresGarageStaffRepository(env.appPool)
	txm := dbpkg.NewTxManager(env.appPool)
	ctx := t.Context()

	t.Run("no tenant context hides every garage", func(t *testing.T) {
		_, err := garages.FindByID(ctx, env.garageA)
		require.ErrorIs(t, err, domain.ErrGarageNotFound)
	})

	t.Run("own garage is visible inside its tenant context", func(t *testing.T) {
		err := txm.WithTransaction(ctx, &env.garageA, func(tx context.Context) error {
			got, err := garages.FindByID(tx, env.garageA)
			require.NoError(t, err)
			require.Equal(t, "garage-"+env.garageA[:8], got.Name)

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("other tenant's garage stays invisible", func(t *testing.T) {
		err := txm.WithTransaction(ctx, &env.garageA, func(tx context.Context) error {
			_, err := garages.FindByID(tx, env.garageB)
			require.ErrorIs(t, err, domain.ErrGarageNotFound)

			list, err := garages.ListByAccountID(tx, env.accountID)
			require.NoError(t, err)
			require.Len(t, list, 1)
			require.Equal(t, env.garageA, list[0].ID)

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("staff lists are scoped to the tenant context", func(t *testing.T) {
		err := txm.WithTransaction(ctx, &env.garageA, func(tx context.Context) error {
			list, err := staff.ListByGarageID(tx, env.garageA)
			require.NoError(t, err)
			require.Len(t, list, 1)
			require.Equal(t, domain.RoleOwner, list[0].Role)

			empty, err := staff.ListByGarageID(tx, env.garageB)
			require.NoError(t, err)
			require.Empty(t, empty)

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("creating a garage inside its own tenant context succeeds", func(t *testing.T) {
		newGarageID := uuid.NewString()

		err := txm.WithTransaction(ctx, &newGarageID, func(tx context.Context) error {
			return garages.Create(tx, &domain.Garage{
				ID:        newGarageID,
				AccountID: env.accountID,
				Name:      "Gara From App Role",
				Timezone:  "UTC",
				Currency:  "VND",
				Status:    domain.GarageStatusActive,
			})
		})
		require.NoError(t, err)
	})

	t.Run("cross-tenant staff creation is rejected by RLS", func(t *testing.T) {
		err := txm.WithTransaction(ctx, &env.garageA, func(tx context.Context) error {
			return staff.Create(tx, &domain.GarageStaff{
				ID:        uuid.NewString(),
				GarageID:  env.garageB,
				AccountID: env.accountID,
				Role:      domain.RoleOwner,
				Status:    domain.StaffStatusActive,
			})
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "row-level security")

		var count int
		err = env.admin.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM garage_staff WHERE garage_id = $1 AND account_id = $2`,
			env.garageB, env.accountID,
		).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}
