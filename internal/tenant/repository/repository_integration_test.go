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
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/stretchr/testify/assert"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
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
	staffID   string
	garageA   string
	garageB   string
}

// startPostgres boots a disposable Postgres, applies the real migrations
// from db/migrations, seeds an owner with two garages and a second
// account, and returns two pools: one bound to the superuser (which
// bypasses RLS) and one bound to a LOGIN member of vietgara_app -- the role
// the backend runs as, subject to every FORCE ROW LEVEL SECURITY policy.
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
	env.staffID = seedAccount(t, ctx, admin, "staff@example.com")

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

// createAppRole creates a login in vietgara_app, whose grants come from the
// migrations themselves.
func createAppRole(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()

	_, err := admin.ExecContext(ctx, `CREATE ROLE `+appRoleName+` LOGIN PASSWORD '`+appRolePass+`' IN ROLE vietgara_app`)
	require.NoError(t, err)
}

func seedAccount(t *testing.T, ctx context.Context, admin *sql.DB, email string) string {
	t.Helper()

	id := uuid.NewString()
	_, err := admin.ExecContext(ctx, `INSERT INTO accounts (id, email, full_name) VALUES ($1, $2, $2)`, id, email)
	require.NoError(t, err)

	return id
}

func seedGarages(t *testing.T, ctx context.Context, admin *sql.DB) (accountID, garageA, garageB string) {
	t.Helper()

	accountID = seedAccount(t, ctx, admin, "owner@example.com")
	garageA, garageB = uuid.NewString(), uuid.NewString()

	for _, id := range []string{garageA, garageB} {
		_, err := admin.ExecContext(ctx, `INSERT INTO garages (id, account_id, name) VALUES ($1, $2, $3)`, id, accountID, "garage-"+id[:8])
		require.NoError(t, err)
		_, err = admin.ExecContext(ctx, `INSERT INTO garage_staff (garage_id, account_id) VALUES ($1, $2)`, id, accountID)
		require.NoError(t, err)
	}

	return accountID, garageA, garageB
}

// TestRLS_TenantModel walks the owner/staff lifecycle through the
// repositories as the application role and checks every RLS boundary.
func TestRLS_TenantModel(t *testing.T) {
	env := startPostgres(t)
	ctx := t.Context()
	tx := dbpkg.NewTxManager(env.appPool)

	garages := repository.NewPostgresGarageRepository(env.appPool)
	staff := repository.NewPostgresStaffRepository(env.appPool)
	roles := repository.NewPostgresRoleRepository(env.appPool)
	groups := repository.NewPostgresGroupRepository(env.appPool)
	invitations := repository.NewPostgresInvitationRepository(env.appPool)
	reader := repository.NewPostgresAccessReader(env.appPool)

	owner := dbpkg.AccountScope(env.accountID)
	member := dbpkg.AccountScope(env.staffID)

	t.Run("nothing is visible without a scope", func(t *testing.T) {
		err := tx.WithTransaction(ctx, dbpkg.Scope{}, func(ctx context.Context) error {
			list, err := garages.ListForAccount(ctx, env.accountID)
			assert.Empty(t, list)

			return err
		})
		require.NoError(t, err)
	})

	var role *domain.Role

	t.Run("the owner lists their garages and manages roles", func(t *testing.T) {
		err := tx.WithTransaction(ctx, owner, func(ctx context.Context) error {
			list, err := garages.ListForAccount(ctx, env.accountID)
			require.NoError(t, err)
			assert.Len(t, list, 2)

			key := domain.DefaultServiceAdvisor
			role = &domain.Role{
				OwnerAccountID: env.accountID, Name: "Advisor", DefaultKey: &key,
				Permissions: []access.Permission{access.CustomerRead, access.CustomerWrite},
			}
			inserted, err := roles.CreateDefault(ctx, role)
			require.NoError(t, err)
			assert.True(t, inserted)

			inserted, err = roles.CreateDefault(ctx, &domain.Role{OwnerAccountID: env.accountID, Name: "Again", DefaultKey: &key})
			require.NoError(t, err)
			assert.False(t, inserted, "one default role per key")

			return nil
		})
		require.NoError(t, err)
	})

	var group *domain.StaffGroup

	t.Run("a new garage gets its owner membership and groups", func(t *testing.T) {
		garage := &domain.Garage{
			ID: uuid.NewString(), OwnerAccountID: env.accountID, Name: "New", Timezone: domain.DefaultTimezone,
			Currency: domain.DefaultCurrency, Status: domain.GarageStatusActive, OperatingHours: domain.DefaultOperatingHours(),
			QuoteNumberTemplate: domain.DefaultQuoteNumberTemplate, RepairOrderNumberTemplate: domain.DefaultRepairOrderNumberTemplate,
			DefaultQuoteApprovalMode: domain.QuoteApprovalModeLink,
		}

		err := tx.WithTransaction(ctx, owner.Garage(garage.ID), func(ctx context.Context) error {
			require.NoError(t, garages.Create(ctx, garage))
			require.NoError(t, staff.Create(ctx, &domain.Staff{GarageID: garage.ID, AccountID: env.accountID, Status: domain.StaffStatusActive}))

			return nil
		})
		require.NoError(t, err)

		err = tx.WithTransaction(ctx, owner.Garage(env.garageA), func(ctx context.Context) error {
			group = &domain.StaffGroup{GarageID: env.garageA, Name: "Front desk", RoleID: role.ID}
			require.NoError(t, groups.Create(ctx, group))

			list, err := groups.List(ctx, env.garageA)
			require.NoError(t, err)
			assert.Len(t, list, 1)
			assert.Equal(t, "Advisor", list[0].RoleName)

			return nil
		})
		require.NoError(t, err)

		err = tx.WithTransaction(ctx, owner.Garage(env.garageA), func(ctx context.Context) error {
			foreign := &domain.Garage{
				ID: uuid.NewString(), OwnerAccountID: env.accountID, Name: "Sneaky", Timezone: "UTC",
				Currency: "VND", Status: domain.GarageStatusActive, DefaultQuoteApprovalMode: domain.QuoteApprovalModeLink,
			}

			return garages.Create(ctx, foreign)
		})
		require.Error(t, err, "a garage can only be inserted in its own scope")
	})

	var invitation *domain.Invitation

	t.Run("the invitee sees the invitation and its garage", func(t *testing.T) {
		err := tx.WithTransaction(ctx, owner.Garage(env.garageA), func(ctx context.Context) error {
			invitation = &domain.Invitation{
				GarageID: env.garageA, AccountID: env.staffID, GroupIDs: []string{group.ID},
				Status: domain.InvitationPending, InvitedBy: env.accountID, ExpiresAt: time.Now().Add(domain.InvitationTTL),
			}

			return invitations.Create(ctx, invitation)
		})
		require.NoError(t, err)

		err = tx.WithTransaction(ctx, member, func(ctx context.Context) error {
			mine, err := invitations.ListByAccount(ctx, env.staffID)
			require.NoError(t, err)
			require.Len(t, mine, 1)
			assert.NotEmpty(t, mine[0].GarageName, "the invited garage is readable")

			_, err = reader.GarageAccess(ctx, env.garageA, env.staffID)
			assert.ErrorIs(t, err, access.ErrNoMembership, "an invitation grants no access")

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("accepting grants the group's permissions", func(t *testing.T) {
		err := tx.WithTransaction(ctx, member.Garage(env.garageA), func(ctx context.Context) error {
			m := &domain.Staff{GarageID: env.garageA, AccountID: env.staffID, Status: domain.StaffStatusActive}
			require.NoError(t, staff.Create(ctx, m))
			require.NoError(t, staff.SetGroups(ctx, env.garageA, m.ID, []string{group.ID}))

			now := time.Now()
			invitation.Status, invitation.RespondedAt = domain.InvitationAccepted, &now

			return invitations.Update(ctx, invitation)
		})
		require.NoError(t, err)

		err = tx.WithTransaction(ctx, member, func(ctx context.Context) error {
			list, err := garages.ListForAccount(ctx, env.staffID)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, env.garageA, list[0].ID)

			acc, err := reader.GarageAccess(ctx, env.garageA, env.staffID)
			require.NoError(t, err)
			assert.False(t, acc.Owner)
			assert.Equal(t, []access.Permission{access.CustomerRead, access.CustomerWrite}, acc.Permissions)

			_, err = reader.GarageAccess(ctx, env.garageB, env.staffID)
			assert.ErrorIs(t, err, access.ErrNoMembership)

			garageCount, staffCount, err := reader.OwnerUsage(ctx, env.accountID)
			require.NoError(t, err)
			assert.Equal(t, 3, garageCount, "owner_usage sees past the caller's scope")
			assert.Equal(t, 1, staffCount)

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("staff cannot cross tenants or edit the owner's roles", func(t *testing.T) {
		err := tx.WithTransaction(ctx, member.Garage(env.garageB), func(ctx context.Context) error {
			return staff.Create(ctx, &domain.Staff{GarageID: env.garageA, AccountID: env.staffID, Status: domain.StaffStatusActive})
		})
		require.Error(t, err, "writes are limited to the scoped garage")

		err = tx.WithTransaction(ctx, member, func(ctx context.Context) error {
			members, err := staff.List(ctx, env.garageB)
			require.NoError(t, err)
			assert.Empty(t, members)

			visible, err := roles.FindByID(ctx, role.ID)
			require.NoError(t, err, "the role of one's own group is readable")

			visible.Name = "Hijacked"

			return roles.Update(ctx, visible)
		})
		require.ErrorIs(t, err, domain.ErrRoleNotFound, "roles are writable by their owner only")
	})

	t.Run("audit log is append-only", func(t *testing.T) {
		err := tx.WithTransaction(ctx, owner, func(ctx context.Context) error {
			q := dbpkg.QuerierFromContext(ctx, env.appPool)
			if _, err := q.Exec(ctx, `INSERT INTO audit_log (action, entity_type, entity_id) VALUES ('x', 'y', 'z')`); err != nil {
				return err
			}

			_, err := q.Exec(ctx, `UPDATE audit_log SET action = 'tampered'`)

			return err
		})
		require.ErrorContains(t, err, "permission denied")
	})
}
