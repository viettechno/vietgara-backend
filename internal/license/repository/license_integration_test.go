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
}

func TestPaymentRepositoryOnPostgres(t *testing.T) {
	pool, admin := startLicenseDB(t)
	ctx := t.Context()

	plans := repository.NewPostgresPlanRepository(pool)
	subs := repository.NewPostgresRepository(pool)

	trial, err := plans.FindByCode(ctx, domain.TrialPlanCode)
	require.NoError(t, err)

	pro, err := plans.FindByCode(ctx, "professional")
	require.NoError(t, err)

	userID := uuid.NewString()
	_, err = admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'payer@example.com', 'Payer')`, userID)
	require.NoError(t, err)

	sub := domain.NewTrial(userID, trial, time.Now().UTC())
	_, err = subs.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)

	stored, err := subs.FindByUserID(ctx, userID)
	require.NoError(t, err)

	t.Run("a request is quoted once per owner", func(t *testing.T) {
		req := &domain.PlanPaymentRequest{
			UserID: userID, SubscriptionID: stored.ID, Kind: domain.PaymentChange, PlanID: pro.ID,
			Status: domain.PaymentAwaitingPayment, Reference: domain.NewReference(), QuotedAmount: pro.Price, AmountDue: pro.Price,
			PeriodDays: 14, UnusedDays: 10, Instructions: domain.PaymentInstructions{BankName: "VCB"},
		}
		require.NoError(t, subs.CreateRequest(ctx, req))
		assert.NotEmpty(t, req.ID)

		again := *req
		again.Reference = domain.NewReference()
		require.ErrorIs(t, subs.CreateRequest(ctx, &again), domain.ErrRequestAlreadyOpen)

		open, err := subs.FindOpenRequest(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, req.ID, open.ID)
		assert.Equal(t, pro.Code, open.Plan.Code)
		assert.Equal(t, "VCB", open.Instructions.BankName)

		found, err := subs.OpenRequests(ctx, []string{stored.ID})
		require.NoError(t, err)
		assert.Contains(t, found, stored.ID)

		_, err = subs.FindRequest(ctx, "not-a-uuid")
		require.ErrorIs(t, err, domain.ErrPaymentRequestNotFound)
	})

	t.Run("a declaration is stored and reviewed", func(t *testing.T) {
		req, err := subs.FindOpenRequest(ctx, userID)
		require.NoError(t, err)

		amount, now := pro.Price-1000, time.Now().UTC()
		req.PayerName, req.TransferredAmount, req.AmountMismatch = "Payer", &amount, true
		req.Status, req.SubmittedAt = domain.PaymentSubmitted, &now
		require.NoError(t, subs.SaveRequest(ctx, req))

		count, oldest, err := subs.WaitingForReview(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.WithinDuration(t, now, oldest, time.Minute)

		page, err := subs.SearchRequests(ctx, usecase.ReviewFilter{Status: domain.PaymentSubmitted, Query: "payer@", Page: 1, PageSize: 10})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)

		mine, err := subs.ListRequests(ctx, userID, "")
		require.NoError(t, err)
		require.Len(t, mine, 1)
		require.NotNil(t, mine[0].TransferredAmount)
		assert.Equal(t, amount, *mine[0].TransferredAmount)
	})

	t.Run("the credit ledger and the settings", func(t *testing.T) {
		require.NoError(t, subs.AddCreditEntry(ctx, &domain.CreditEntry{
			UserID: userID, Kind: domain.CreditEarned, Amount: 5000, BalanceAfter: 5000, Note: "unused",
		}))

		entries, err := subs.ListCredit(ctx, userID)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, int64(5000), entries[0].BalanceAfter)

		settings, err := subs.Settings(ctx)
		require.NoError(t, err)

		settings.BankName = "Vietcombank"
		require.NoError(t, subs.SaveSettings(ctx, settings, ""))

		settings, err = subs.Settings(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Vietcombank", settings.BankName)

		now := time.Now().UTC()
		claimed, err := subs.ClaimReviewReminder(ctx, now, 24*time.Hour)
		require.NoError(t, err)
		assert.True(t, claimed)

		claimed, err = subs.ClaimReviewReminder(ctx, now.Add(time.Hour), 24*time.Hour)
		require.NoError(t, err)
		assert.False(t, claimed, "once a day")

		claimed, err = subs.ClaimReviewReminder(ctx, now.Add(25*time.Hour), 24*time.Hour)
		require.NoError(t, err)
		assert.True(t, claimed)
	})
}
