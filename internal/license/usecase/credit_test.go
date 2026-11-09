package usecase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/licensetest"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
)

// payNow starts, declares and approves a payment, as the owner and an admin do.
func payNow(t *testing.T, e *licensetest.Env, planID string, kind domain.PaymentKind) *domain.Subscription {
	t.Helper()

	ctx := t.Context()

	v, err := e.Payments.Start(ctx, "user-1", planID, kind)
	require.NoError(t, err)

	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, declare(v.Request.AmountDue))
	require.NoError(t, err)

	sub, err := e.Payments.Approve(ctx, "admin", v.Request.ID, "ok")
	require.NoError(t, err)

	return sub
}

// Credit lapses one year after it was earned and payments use the oldest first.
func TestCreditIsUsedOldestFirstAndLapsesAfterAYear(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)
	sub := payNow(t, e, "p-pro", domain.PaymentChange)
	first := e.Now

	_, err := e.Payments.AdjustCredit(ctx, "admin", sub.ID, 500000, "lot A")
	require.NoError(t, err)

	e.Now = e.Now.AddDate(0, 6, 0)

	_, err = e.Payments.AdjustCredit(ctx, "admin", sub.ID, 300000, "lot B")
	require.NoError(t, err)

	got, _, err := e.Subscriptions.GetMine(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, int64(800000), got.CreditBalance)
	require.NotNil(t, got.NextCreditExpiry)
	assert.Equal(t, first.AddDate(1, 0, 0), *got.NextCreditExpiry, "the next lot to lapse is the oldest")

	// A renewal of the 599,000 plan takes all of A and 99,000 of B.
	renewed := payNow(t, e, "p-pro", domain.PaymentRenew)
	assert.Equal(t, int64(201000), renewed.CreditBalance)

	got, _, err = e.Subscriptions.GetMine(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, e.Now.AddDate(1, 0, 0), *got.NextCreditExpiry, "lot A is used up, so lot B is next")

	// Past B's year the rest lapses when the job runs.
	e.Now = e.Now.AddDate(1, 0, 0)
	require.NoError(t, e.Expiry.Run(ctx))

	got, _, err = e.Subscriptions.GetMine(ctx, "user-1")
	require.NoError(t, err)
	assert.Zero(t, got.CreditBalance)
	assert.Nil(t, got.NextCreditExpiry)

	ledger := e.Store.Ledger()
	last := ledger[len(ledger)-1]
	assert.Equal(t, domain.CreditExpired, last.Kind)
	assert.Equal(t, int64(-201000), last.Amount)
	assert.Contains(t, e.Store.Events, usecase.EventCreditChanged)

	require.NoError(t, e.Expiry.Run(ctx), "nothing is left to lapse")
	assert.Len(t, e.Store.Ledger(), len(ledger))
}

// A quote counts only credit that has not lapsed, and an approval refuses the
// credit of a quote that lapsed since (FR-LIC-08, FR-LIC-09).
func TestLapsedCreditIsNeverApplied(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)
	sub := payNow(t, e, "p-pro", domain.PaymentChange)

	_, err := e.Payments.AdjustCredit(ctx, "admin", sub.ID, 500000, "goodwill")
	require.NoError(t, err)

	quoted, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentRenew)
	require.NoError(t, err)
	assert.Equal(t, int64(500000), quoted.Request.CreditApplied)

	_, err = e.Payments.Submit(ctx, "user-1", quoted.Request.ID, declare(quoted.Request.AmountDue))
	require.NoError(t, err)

	// The credit lapses before the admin looks at the request.
	e.Now = e.Now.AddDate(1, 1, 0)

	_, err = e.Payments.Approve(ctx, "admin", quoted.Request.ID, "late")
	requireReason(t, err, domain.ReasonCreditExpired)

	got, err := e.Payments.GetMine(ctx, "user-1", quoted.Request.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentSubmitted, got.Request.Status, "the request stays waiting")

	// The owner is quoted again, now without the lapsed credit.
	_, err = e.Payments.Cancel(ctx, "user-1", quoted.Request.ID)
	require.NoError(t, err)

	again, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentRenew)
	require.NoError(t, err)
	assert.Zero(t, again.Request.CreditApplied)
	assert.Equal(t, again.Request.QuotedAmount, again.Request.AmountDue)
}

func TestCashPayment(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	cash := func(planID string) usecase.RequestView {
		v, err := e.Payments.Start(ctx, "user-1", planID, domain.PaymentChange)
		require.NoError(t, err)
		require.Positive(t, v.Request.AmountDue)

		return v
	}

	v := cash("p-pro")

	// A bank transfer still needs its evidence.
	_, err := e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{PayerName: "A", TransferredAmount: v.Request.AmountDue})
	requireReason(t, err, domain.ReasonEvidenceRequired)

	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{
		Method: "card", PayerName: "A", TransferredAmount: v.Request.AmountDue,
	})
	require.Error(t, err, "an unknown method")

	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{
		Method: domain.PaymentCash, PayerName: "Nguyen Van A", TransferredAmount: v.Request.AmountDue,
	})
	require.Error(t, err, "cash needs the date it was handed over")

	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{
		Method: domain.PaymentBankTransfer, PayerName: "Nguyen Van A", TransferredAmount: v.Request.AmountDue,
		CashNote: "not a cash payment",
	})
	require.ErrorIs(t, err, domain.ErrInvalidDeclaration, "cash notes are only valid for cash payments")

	paid := e.Now
	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{
		Method: domain.PaymentCash, PayerName: "Nguyen Van A", TransferredAmount: v.Request.AmountDue,
		PaidAt: &paid, CashNote: strings.Repeat("💵", 2001),
	})
	require.ErrorIs(t, err, domain.ErrInvalidDeclaration, "cash notes are limited by Unicode code points")

	submitted, err := e.Payments.Submit(ctx, "user-1", v.Request.ID, usecase.Declaration{
		Method: domain.PaymentCash, PayerName: "Nguyen Van A", PayerBank: "ignored", TransferredAmount: v.Request.AmountDue,
		PaidAt: &paid, CashNote: "  Received at the front desk  ",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentCash, submitted.Request.PaymentMethod)
	assert.Equal(t, "Received at the front desk", submitted.Request.CashNote)
	assert.Empty(t, submitted.Request.PayerBank, "a cash payment has no bank details")
	assert.Empty(t, submitted.Request.EvidenceFileID)
	assert.False(t, submitted.Request.AmountMismatch)

	sub, err := e.Payments.Approve(ctx, "admin", v.Request.ID, "received in cash")
	require.NoError(t, err)
	assert.Equal(t, "p-pro", sub.PlanID)
	assert.Equal(t, domain.StatusActive, sub.Status)
}

// Only the kinds a plan counts use its daily limit (FR-LIC-02).
func TestOnlyCountedKindsUseTheDailyLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()

	trial, err := e.Store.Plans().FindByCode(ctx, domain.TrialPlanCode)
	require.NoError(t, err)

	trial.MaxTransactionsPerDay = 1
	trial.CountedKinds = []domain.TransactionKind{domain.TransactionRepairOrder}
	require.NoError(t, e.Store.Plans().Update(ctx, trial))

	for range 3 {
		require.NoError(t, e.Entitlements.ConsumeTransaction(ctx, "owner", "quote"), "quotes are not counted")
		require.NoError(t, e.Entitlements.ConsumeTransaction(ctx, "owner", "settlement"), "nor are settlements")
	}

	require.NoError(t, e.Entitlements.ConsumeTransaction(ctx, "owner", "repair_order"))
	requireReason(t, e.Entitlements.ConsumeTransaction(ctx, "owner", "repair_order"), domain.ReasonPlanLimitReached)

	usage, err := e.Subscriptions.Usage(ctx, "owner")
	require.NoError(t, err)
	assert.Equal(t, 2, usage.TransactionsToday, "the shared counter went past the limit once")

	// The admin makes quotes count too.
	trial.CountedKinds = []domain.TransactionKind{domain.TransactionRepairOrder, domain.TransactionQuote}
	require.NoError(t, e.Store.Plans().Update(ctx, trial))
	require.NoError(t, e.Entitlements.Invalidate(ctx, "owner"))

	requireReason(t, e.Entitlements.ConsumeTransaction(ctx, "owner", "quote"), domain.ReasonPlanLimitReached)
}

func TestPlanCycleIsLockedWhileSubscribed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()

	plan, err := e.Subscriptions.CreatePlan(ctx, usecase.PlanFields{
		Code: new("enterprise"), Name: new("Enterprise"), MaxGarages: new(20), MaxStaff: new(200), MaxTransactionsPerDay: new(2000),
	})
	require.NoError(t, err)
	assert.Equal(t, domain.BillingCycleYearly, plan.BillingCycle, "plans are yearly by default")
	assert.Equal(t, []domain.TransactionKind{domain.TransactionRepairOrder}, plan.CountedKinds, "repair orders count by default")

	monthly := domain.BillingCycleMonthly
	changed, err := e.Subscriptions.UpdatePlan(ctx, plan.ID, usecase.PlanFields{BillingCycle: &monthly})
	require.NoError(t, err, "nobody uses the plan")
	assert.Equal(t, domain.BillingCycleMonthly, changed.BillingCycle)

	// The trial plan has a subscriber now.
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "user-1"))

	yearly := domain.BillingCycleYearly
	_, err = e.Subscriptions.UpdatePlan(ctx, "p-trial", usecase.PlanFields{BillingCycle: &yearly})
	requireReason(t, err, domain.ReasonPlanHasSubscribers)

	// Everything else may change, and a cycle sent unchanged is not a change.
	trialCycle := domain.BillingCycleMonthly
	_, err = e.Subscriptions.UpdatePlan(ctx, "p-trial", usecase.PlanFields{BillingCycle: &trialCycle, Price: new(int64(0)), MaxStaff: new(6)})
	require.NoError(t, err)
}

func TestTrialLengthFollowsTheTrialPlan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "early"))

	early, _, err := e.Subscriptions.GetMine(ctx, "early")
	require.NoError(t, err)
	assert.Equal(t, e.Now.AddDate(0, 0, 60), *early.TrialEndAt, "two months by default")

	_, err = e.Subscriptions.UpdatePlan(ctx, "p-trial", usecase.PlanFields{TrialDays: new(14)})
	require.NoError(t, err)

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "late"))

	late, _, err := e.Subscriptions.GetMine(ctx, "late")
	require.NoError(t, err)
	assert.Equal(t, e.Now.AddDate(0, 0, 14), *late.TrialEndAt)

	again, _, err := e.Subscriptions.GetMine(ctx, "early")
	require.NoError(t, err)
	assert.Equal(t, early.TrialEndAt, again.TrialEndAt, "a running trial keeps its end")

	_, err = e.Subscriptions.UpdatePlan(ctx, "p-trial", usecase.PlanFields{TrialDays: new(0)})
	require.Error(t, err, "the trial plan needs a length")
	_, err = e.Subscriptions.UpdatePlan(ctx, "p-starter", usecase.PlanFields{TrialDays: new(14)})
	require.Error(t, err, "other plans have no trial")
}

func TestSupportContactDetails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()

	phone, mail, zalo := "0900 000 000", "support@vietgara.vn", "0900000000"
	_, err := e.Payments.UpdateSettings(ctx, "admin", usecase.SettingsFields{SupportPhone: &phone, SupportEmail: &mail, SupportZalo: &zalo})
	require.NoError(t, err)

	instructions, _, err := e.Payments.Instructions(ctx)
	require.NoError(t, err)
	assert.Equal(t, phone, instructions.SupportPhone)
	assert.Equal(t, mail, instructions.SupportEmail)
	assert.Equal(t, zalo, instructions.SupportZalo)

	long := "0900000000000000000000000000000000000000000000000000000"
	_, err = e.Payments.UpdateSettings(ctx, "admin", usecase.SettingsFields{SupportPhone: &long})
	require.Error(t, err, "longer than the column")
}

// An entitlement cached before the counted kinds existed is read again from
// the database instead of letting every kind through.
func TestStaleCachedEntitlementsAreReloaded(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "owner"))

	require.NoError(t, e.Cache.Set(ctx, &domain.Entitlements{UserID: "owner", Status: domain.StatusTrial, MaxTransactionsPerDay: 1}))

	got, err := e.Entitlements.Get(ctx, "owner")
	require.NoError(t, err)
	assert.Equal(t, domain.AllTransactionKinds, got.CountedKinds, "the stale entry was replaced")
}
