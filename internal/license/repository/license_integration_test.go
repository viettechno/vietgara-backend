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

	userID := uuid.NewString()
	_, err = admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'Owner@Example.com', 'Minh')`, userID)
	require.NoError(t, err)

	sub := domain.NewTrial(userID, trial, time.Now().UTC())
	inserted, err := subs.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = subs.CreateIfAbsent(ctx, domain.NewTrial(userID, trial, time.Now().UTC()))
	require.NoError(t, err)
	assert.False(t, inserted)

	require.NoError(t, subs.Replace(ctx, userID, domain.FeatureFlags(sub)))
	require.NoError(t, subs.Replace(ctx, userID, domain.FeatureFlags(sub)), "upsert")

	locked, err := subs.LockByUserID(ctx, userID)
	require.NoError(t, err)
	require.NoError(t, locked.Extend(5, time.Now().UTC()))
	require.NoError(t, subs.Update(ctx, locked))

	due, err := subs.ListUserIDsDueBefore(ctx, time.Now().Add(30*24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, due, userID)

	page, err := subs.Search(ctx, usecase.SubscriptionFilter{Query: "owner@", Status: domain.StatusTrial, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	assert.Equal(t, "Minh", page.Items[0].FullName)

	var enabled int
	require.NoError(t, admin.QueryRowContext(ctx, `SELECT count(*) FROM entitlements WHERE user_id = $1 AND enabled`, userID).Scan(&enabled))
	assert.Equal(t, 7, enabled, "the trial enables every feature")

	checkPlanChanges(t, pool, admin, userID, locked.ID, locked.CurrentPeriodEnd)
}

// checkPlanChanges runs a plan change through its states on Postgres: one
// pending per subscription, loaded with the subscription and its plan, and
// found by the expiry job's scan.
func checkPlanChanges(t *testing.T, pool *dbpkg.Pool, admin *sql.DB, userID, subscriptionID string, periodEnd time.Time) {
	t.Helper()

	ctx := t.Context()
	plans := repository.NewPostgresPlanRepository(pool)
	subs := repository.NewPostgresRepository(pool)

	starter, err := plans.FindByCode(ctx, "starter")
	require.NoError(t, err)

	change := &domain.PlanChange{
		SubscriptionID: subscriptionID, PlanID: starter.ID, Status: domain.PlanChangeScheduled,
		EffectiveAt: periodEnd, RequestedAt: time.Now().UTC(),
	}
	require.NoError(t, subs.CreateChange(ctx, change))
	require.NotEmpty(t, change.ID)

	second := *change
	second.ID = ""
	require.Error(t, subs.CreateChange(ctx, &second), "only one change is pending per subscription")

	withChange, err := subs.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, withChange.PendingChange)
	assert.Equal(t, "starter", withChange.PendingChange.Plan.Code)
	assert.Equal(t, domain.PlanChangeScheduled, withChange.PendingChange.Status)

	page, err := subs.Search(ctx, usecase.SubscriptionFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.NotNil(t, page.Items[0].Subscription.PendingChange)

	// Due once its date passed, even when the subscription is no longer live.
	_, err = admin.ExecContext(ctx, `UPDATE subscriptions SET status = 'suspended', current_period_end = now() - interval '1 day' WHERE user_id = $1`, userID)
	require.NoError(t, err)
	_, err = admin.ExecContext(ctx, `UPDATE subscription_changes SET effective_at = now() - interval '1 day' WHERE id = $1`, change.ID)
	require.NoError(t, err)

	due, err := subs.ListUserIDsDueBefore(ctx, time.Now())
	require.NoError(t, err)
	assert.Contains(t, due, userID)

	// Moving it, postponing it and cancelling it.
	postponed := time.Now().UTC()
	change.Status, change.PostponedAt = domain.PlanChangePostponed, &postponed
	require.NoError(t, subs.SaveChange(ctx, change))

	again, err := subs.FindByUserID(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.PlanChangePostponed, again.PendingChange.Status)

	cancelled := time.Now().UTC()
	change.Status, change.CancelledAt = domain.PlanChangeCancelled, &cancelled
	require.NoError(t, subs.SaveChange(ctx, change))

	gone, err := subs.FindByUserID(ctx, userID)
	require.NoError(t, err)
	assert.Nil(t, gone.PendingChange)

	// A new one can be scheduled again after the cancellation.
	next := &domain.PlanChange{
		SubscriptionID: subscriptionID, PlanID: starter.ID, Status: domain.PlanChangeScheduled,
		EffectiveAt: time.Now().Add(time.Hour), RequestedAt: time.Now().UTC(),
	}
	require.NoError(t, subs.CreateChange(ctx, next))
}
