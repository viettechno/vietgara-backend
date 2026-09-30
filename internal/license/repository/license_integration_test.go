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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/repository"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// startLicenseDB boots Postgres with every migration applied and returns a
// pool of a login in vietgara_app plus the superuser connection.
func startLicenseDB(t *testing.T) (*dbpkg.Pool, *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:15-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_DB": "license_test", "POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	admin, err := sql.Open("postgres", fmt.Sprintf("postgres://postgres:postgres@%s:%s/license_test?sslmode=disable", host, port.Port()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	dir := filepath.Join("..", "..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var up []string

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			up = append(up, e.Name())
		}
	}

	sort.Strings(up)

	for _, name := range up {
		content, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		_, err = admin.ExecContext(ctx, string(content))
		require.NoError(t, err, name)
	}

	_, err = admin.ExecContext(ctx, `CREATE ROLE license_app LOGIN PASSWORD 'app' IN ROLE vietgara_app`)
	require.NoError(t, err)

	conn, err := dbpkg.NewPgxPool(ctx, fmt.Sprintf("postgres://license_app:app@%s:%s/license_test?sslmode=disable", host, port.Port()))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	return dbpkg.NewPool(ctx, conn), admin
}

func TestLicenseRepositoriesOnPostgres(t *testing.T) {
	pool, admin := startLicenseDB(t)
	ctx := t.Context()

	plans := repository.NewPostgresPlanRepository(pool)
	subs := repository.NewPostgresRepository(pool)

	public, err := plans.ListPublic(ctx)
	require.NoError(t, err)
	require.Len(t, public, 3, "seeded starter, professional and chain")
	assert.Equal(t, 2, public[1].MaxGarages)

	plan := &domain.Plan{
		Code: "garage-plus", Name: "Garage+", Currency: "VND", BillingCycle: domain.BillingCycleYearly,
		MaxGarages: 3, MaxStaff: 10, MaxTransactionsPerDay: 200, Features: []domain.Feature{domain.FeatureQuotes, domain.FeatureReports},
		Public: true, Active: true,
	}
	require.NoError(t, plans.Create(ctx, plan))
	require.ErrorIs(t, plans.Create(ctx, plan), domain.ErrPlanCodeTaken)

	plan.Active = false
	plan.Features = []domain.Feature{domain.FeatureInventory}
	require.NoError(t, plans.Update(ctx, plan))

	stored, err := plans.FindByCode(ctx, "garage-plus")
	require.NoError(t, err)
	assert.False(t, stored.Active)
	assert.Equal(t, []domain.Feature{domain.FeatureInventory}, stored.Features)

	trial, err := plans.FindByCode(ctx, domain.TrialPlanCode)
	require.NoError(t, err)

	accountID := uuid.NewString()
	_, err = admin.ExecContext(ctx, `INSERT INTO accounts (id, email, full_name) VALUES ($1, 'Owner@Example.com', 'Minh')`, accountID)
	require.NoError(t, err)

	sub := domain.NewTrial(accountID, trial, time.Now().UTC())
	inserted, err := subs.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = subs.CreateIfAbsent(ctx, domain.NewTrial(accountID, trial, time.Now().UTC()))
	require.NoError(t, err)
	assert.False(t, inserted)

	require.NoError(t, subs.Replace(ctx, accountID, domain.FeatureFlags(sub)))
	require.NoError(t, subs.Replace(ctx, accountID, domain.FeatureFlags(sub)), "upsert")

	locked, err := subs.LockByAccountID(ctx, accountID)
	require.NoError(t, err)
	require.NoError(t, locked.Extend(5, time.Now().UTC()))
	require.NoError(t, subs.Update(ctx, locked))

	due, err := subs.ListAccountIDsDueBefore(ctx, time.Now().Add(30*24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, due, accountID)

	page, err := subs.Search(ctx, usecase.SubscriptionFilter{Query: "owner@", Status: domain.StatusTrial, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	assert.Equal(t, "Minh", page.Items[0].FullName)

	var enabled int
	require.NoError(t, admin.QueryRowContext(ctx, `SELECT count(*) FROM entitlements WHERE account_id = $1 AND enabled`, accountID).Scan(&enabled))
	assert.Equal(t, 7, enabled, "the trial enables every feature")
}
