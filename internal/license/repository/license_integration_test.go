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
	assert.Equal(t, 3, public[1].MaxGarages)
	assert.Equal(t, domain.BillingCycleYearly, public[1].BillingCycle)
	assert.ElementsMatch(t, domain.AllFeatures, public[0].Features, "every plan has every feature")
	assert.Equal(t, []domain.TransactionKind{domain.TransactionRepairOrder}, public[1].CountedKinds)

	plan := &domain.Plan{
		Code: "garage-plus", Name: "Garage+", Currency: "VND", BillingCycle: domain.BillingCycleYearly,
		MaxGarages: 3, MaxStaff: 10, MaxTransactionsPerDay: 200, CountedKinds: []domain.TransactionKind{domain.TransactionQuote},
		Features: []domain.Feature{domain.FeatureQuotes, domain.FeatureReports},
		Public:   true, Active: true,
	}
	require.NoError(t, plans.Create(ctx, plan))
	require.ErrorIs(t, plans.Create(ctx, plan), domain.ErrPlanCodeTaken)

	plan.Active = false
	plan.Features = []domain.Feature{domain.FeatureInventory}
	require.NoError(t, plans.Update(ctx, plan))

	stored, err := plans.FindByCode(ctx, "garage-plus")
	require.NoError(t, err)
	assert.False(t, stored.Active)
	assert.Equal(t, []domain.TransactionKind{domain.TransactionQuote}, stored.CountedKinds)
	assert.Zero(t, stored.TrialDays)
	assert.Equal(t, []domain.Feature{domain.FeatureInventory}, stored.Features)

	trial, err := plans.FindByCode(ctx, domain.TrialPlanCode)
	require.NoError(t, err)
	assert.Equal(t, 60, trial.TrialDays, "the trial lasts two months")

	subscribers, err := plans.CountSubscribers(ctx, trial.ID)
	require.NoError(t, err)
	assert.Zero(t, subscribers)

	userID := uuid.NewString()
	_, err = admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'Owner@Example.com', 'Minh')`, userID)
	require.NoError(t, err)

	sub := domain.NewTrial(userID, trial, time.Now().UTC())
	inserted, err := subs.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)
	require.True(t, inserted)
	assert.Equal(t, 60*24*time.Hour, sub.TrialEndAt.Sub(sub.CurrentPeriodStart))

	subscribers, err = plans.CountSubscribers(ctx, trial.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, subscribers)

	owners, err := plans.ListSubscriberIDs(ctx, trial.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{userID}, owners)

	inserted, err = subs.CreateIfAbsent(ctx, domain.NewTrial(userID, trial, time.Now().UTC()))
	require.NoError(t, err)
	assert.False(t, inserted)

	require.NoError(t, subs.Replace(ctx, userID, domain.FeatureFlags(sub)))
	require.NoError(t, subs.Replace(ctx, userID, domain.FeatureFlags(sub)), "upsert")

	locked, err := subs.LockByUserID(ctx, userID)
	require.NoError(t, err)
	require.NoError(t, locked.Extend(5, time.Now().UTC()))
	require.NoError(t, subs.Update(ctx, locked))

	due, err := subs.ListUserIDsDueBefore(ctx, time.Now().Add(90*24*time.Hour))
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

		// The entries of one approval share a created_at: the order they were
		// written in decides, newest first.
		same := time.Now().UTC()
		for _, balance := range []int64{7000, 6000, 1000} {
			_, execErr := admin.ExecContext(ctx, `INSERT INTO credit_ledger (user_id, kind, amount, balance_after, created_at)
				VALUES ($1, 'adjustment', 1, $2, $3)`, userID, balance, same)
			require.NoError(t, execErr)
		}

		ordered, err := subs.ListCredit(ctx, userID)
		require.NoError(t, err)
		require.Len(t, ordered, 4)
		assert.Equal(t, []int64{1000, 6000, 7000}, []int64{ordered[0].BalanceAfter, ordered[1].BalanceAfter, ordered[2].BalanceAfter}, "newest written first, whatever the ids")

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

func TestCreditLotsOnPostgres(t *testing.T) {
	pool, admin := startLicenseDB(t)
	ctx := t.Context()
	subs := repository.NewPostgresRepository(pool)

	lotUser := uuid.NewString()
	_, err := admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'lots@example.com', 'Lots')`, lotUser)
	require.NoError(t, err)

	now := time.Now().UTC()
	lot := func(amount int64, expires time.Time) {
		require.NoError(t, subs.AddCreditEntry(ctx, &domain.CreditEntry{
			UserID: lotUser, Kind: domain.CreditEarned, Amount: amount, BalanceAfter: amount,
			ExpiresAt: &expires, Remaining: amount,
		}))
	}

	lot(300000, now.AddDate(0, 6, 0)) // later
	lot(500000, now.AddDate(0, 2, 0)) // sooner, written second
	lot(100000, now.Add(-time.Hour))  // already lapsed

	available, err := subs.AvailableCredit(ctx, lotUser, now)
	require.NoError(t, err)
	assert.Equal(t, int64(800000), available, "the lapsed lot does not count")

	next, err := subs.NextCreditExpiry(ctx, lotUser, now)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.WithinDuration(t, now.AddDate(0, 2, 0), *next, time.Second)

	require.ErrorIs(t, subs.TakeCredit(ctx, lotUser, 900000, now), domain.ErrCreditExpired)

	after, err := subs.AvailableCredit(ctx, lotUser, now)
	require.NoError(t, err)
	assert.Equal(t, int64(800000), after, "a refused take changes nothing")

	require.NoError(t, subs.TakeCredit(ctx, lotUser, 600000, now))

	var sooner, later int64
	require.NoError(t, admin.QueryRowContext(ctx, `SELECT remaining FROM credit_ledger WHERE user_id = $1 AND amount = 500000`, lotUser).Scan(&sooner))
	require.NoError(t, admin.QueryRowContext(ctx, `SELECT remaining FROM credit_ledger WHERE user_id = $1 AND amount = 300000`, lotUser).Scan(&later))
	assert.Zero(t, sooner, "the sooner lot is used first")
	assert.Equal(t, int64(200000), later)

	users, err := subs.UsersWithLapsedCredit(ctx, now, "", 10)
	require.NoError(t, err)
	assert.Equal(t, []string{lotUser}, users)

	users, err = subs.UsersWithLapsedCredit(ctx, now, lotUser, 10)
	require.NoError(t, err)
	assert.Empty(t, users, "paging continues after the last user seen")

	lapsed, err := subs.LapseCredit(ctx, lotUser, now)
	require.NoError(t, err)
	assert.Equal(t, int64(100000), lapsed)

	lapsed, err = subs.LapseCredit(ctx, lotUser, now)
	require.NoError(t, err)
	assert.Zero(t, lapsed, "lapsing is done once")

	users, err = subs.UsersWithLapsedCredit(ctx, now, "", 10)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestSupportContactOnPostgres(t *testing.T) {
	pool, _ := startLicenseDB(t)
	ctx := t.Context()
	subs := repository.NewPostgresRepository(pool)

	settings, err := subs.Settings(ctx)
	require.NoError(t, err)
	assert.Empty(t, settings.SupportPhone, "nothing is set at first")

	settings.SupportPhone, settings.SupportEmail, settings.SupportZalo = "0900000000", "help@vietgara.vn", "0900000001"
	require.NoError(t, subs.SaveSettings(ctx, settings, ""))

	settings, err = subs.Settings(ctx)
	require.NoError(t, err)
	assert.Equal(t, "0900000000", settings.SupportPhone)
	assert.Equal(t, "help@vietgara.vn", settings.SupportEmail)
	assert.Equal(t, "0900000001", settings.SupportZalo)
}

func TestCashNoteOnPostgres(t *testing.T) {
	pool, admin := startLicenseDB(t)
	ctx := t.Context()
	plans := repository.NewPostgresPlanRepository(pool)
	subs := repository.NewPostgresRepository(pool)

	userID := uuid.NewString()
	_, err := admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Payer')`, userID, userID+"@example.test")
	require.NoError(t, err)

	trial, err := plans.FindByCode(ctx, domain.TrialPlanCode)
	require.NoError(t, err)
	pro, err := plans.FindByCode(ctx, "professional")
	require.NoError(t, err)

	sub := domain.NewTrial(userID, trial, time.Now().UTC())
	_, err = subs.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)

	now, amount := time.Now().UTC(), pro.Price
	req := &domain.PlanPaymentRequest{
		UserID: userID, SubscriptionID: sub.ID, Kind: domain.PaymentChange, PlanID: pro.ID,
		Status: domain.PaymentAwaitingPayment, Reference: domain.NewReference(), QuotedAmount: pro.Price, AmountDue: pro.Price,
		PeriodDays: 365, Instructions: domain.PaymentInstructions{BankName: "VCB"},
	}
	require.NoError(t, subs.CreateRequest(ctx, req))

	req.Status, req.PaymentMethod, req.CashNote = domain.PaymentSubmitted, domain.PaymentCash, "received at the counter"
	req.PayerName, req.TransferredAmount, req.PaidAt, req.SubmittedAt = "Payer", &amount, &now, &now
	require.NoError(t, subs.SaveRequest(ctx, req))

	stored, err := subs.FindRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, "received at the counter", stored.CashNote)
}

// The expiry job pages through lapsed owners by uuid: every owner appears
// once, in uuid order, whatever the database collation.
func TestLapsedCreditPagingOnPostgres(t *testing.T) {
	pool, admin := startLicenseDB(t)
	ctx := t.Context()
	subs := repository.NewPostgresRepository(pool)
	now := time.Now().UTC()

	for i := range 5 {
		id := uuid.NewString()
		_, err := admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Lapsed')`, id, id+"@example.test")
		require.NoError(t, err)
		_, err = admin.ExecContext(ctx, `INSERT INTO credit_ledger (user_id, kind, amount, balance_after, expires_at, remaining)
			VALUES ($1, 'earned', $2, $2, $3, $2)`, id, int64(1000+i), now.Add(-time.Hour))
		require.NoError(t, err)
	}

	// The expected order is the database's own order of the uuid column.
	rows, err := admin.QueryContext(ctx, `SELECT user_id::text FROM credit_ledger ORDER BY user_id`)
	require.NoError(t, err)

	defer rows.Close()

	var want []string

	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))

		want = append(want, id)
	}

	require.NoError(t, rows.Err())
	require.Len(t, want, 5)

	var got []string

	after := ""

	for {
		page, err := subs.UsersWithLapsedCredit(ctx, now, after, 2)
		require.NoError(t, err)

		got = append(got, page...)

		if len(page) < 2 {
			break
		}

		after = page[len(page)-1]
	}

	assert.Equal(t, want, got, "each owner once, in uuid order")
}
