//go:build integration

package repository_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/repository"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// startLicenseDB boots Postgres with every migration applied and returns a
// pool of a login in vietgara_app plus the superuser connection.
func startLicenseDB(t *testing.T) (*dbpkg.Pool, *sql.DB) {
	t.Helper()

	pg := pgtest.StartMigrated(t)

	return pg.AppPool(t), pg.Admin
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
