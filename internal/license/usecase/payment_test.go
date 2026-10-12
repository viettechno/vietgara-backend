package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/licensetest"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var png = []byte("\x89PNG screenshot")

func declare(amount int64) usecase.Declaration {
	return usecase.Declaration{
		PayerName: "Nguyen Van A", TransferredAmount: amount, Evidence: png, EvidenceContentType: "image/png",
	}
}

func trialOwner(t *testing.T) *licensetest.Env {
	t.Helper()

	e := licensetest.NewEnv()
	require.NoError(t, e.Subscriptions.EnsureTrial(t.Context(), "user-1"))

	return e
}

func TestStartRequest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	view, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentAwaitingPayment, view.Request.Status)
	assert.Equal(t, int64(599000), view.Request.AmountDue)
	assert.Regexp(t, `^VG-`, view.Request.Reference)

	t.Run("one open request per owner", func(t *testing.T) {
		_, err := e.Payments.Start(ctx, "user-1", "p-starter", domain.PaymentChange)
		require.NoError(t, err, "an unpaid quote is replaced")

		open, err := e.Payments.OpenRequest(ctx, "user-1")
		require.NoError(t, err)
		require.NotNil(t, open)
		assert.Equal(t, "p-starter", open.Request.PlanID)

		_, err = e.Payments.Submit(ctx, "user-1", open.Request.ID, declare(299000))
		require.NoError(t, err)

		_, err = e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
		requireReason(t, err, domain.ReasonRequestAlreadyOpen)
	})

	t.Run("the plan must be offered", func(t *testing.T) {
		e := trialOwner(t)

		_, err := e.Payments.Start(ctx, "user-1", "p-legacy", domain.PaymentChange)
		requireReason(t, err, domain.ReasonPlanNotAvailable)

		_, err = e.Payments.Start(ctx, "user-1", "missing", domain.PaymentChange)
		requireReason(t, err, apperror.ReasonNotFound)

		_, err = e.Payments.Start(ctx, "user-1", "p-pro", "bogus")
		require.Error(t, err)
	})

	t.Run("the limits are checked at submission", func(t *testing.T) {
		e := trialOwner(t)
		e.Store.Garages["user-1"] = 2

		_, err := e.Payments.Start(ctx, "user-1", "p-starter", domain.PaymentChange)
		requireReason(t, err, domain.ReasonPlanLimitReached)
	})
}

func TestSubmitAndApprove(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	view, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
	require.NoError(t, err)

	t.Run("the transfer needs evidence", func(t *testing.T) {
		_, err := e.Payments.Submit(ctx, "user-1", view.Request.ID, usecase.Declaration{PayerName: "A", TransferredAmount: 599000})
		requireReason(t, err, domain.ReasonEvidenceRequired)

		_, err = e.Payments.Submit(ctx, "user-1", view.Request.ID, usecase.Declaration{TransferredAmount: 599000, Evidence: png, EvidenceContentType: "image/png"})
		require.Error(t, err, "the payer is named")

		bad := declare(599000)
		bad.EvidenceContentType = "text/plain"
		_, err = e.Payments.Submit(ctx, "user-1", view.Request.ID, bad)
		require.Error(t, err)
	})

	t.Run("only the owner sees the request", func(t *testing.T) {
		_, err := e.Payments.GetMine(ctx, "someone", view.Request.ID)
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("a mismatch is flagged and needs a note to approve", func(t *testing.T) {
		submitted, err := e.Payments.Submit(ctx, "user-1", view.Request.ID, declare(500000))
		require.NoError(t, err)
		assert.True(t, submitted.Request.AmountMismatch)
		assert.NotEmpty(t, submitted.EvidenceURL)
		assert.Contains(t, e.Store.Events, usecase.EventPaymentSubmitted)

		_, err = e.Payments.Approve(ctx, "admin", view.Request.ID, "")
		requireReason(t, err, domain.ReasonMismatchNoteRequired)

		sub, err := e.Payments.Approve(ctx, "admin", view.Request.ID, "Customer confirmed by phone")
		require.NoError(t, err)
		assert.Equal(t, domain.StatusActive, sub.Status)
		assert.Equal(t, "p-pro", sub.PlanID)
		assert.Equal(t, int64(599000), sub.PeriodPaidAmount)
		assert.Equal(t, e.Now.AddDate(1, 0, 0), sub.CurrentPeriodEnd)
		assert.Contains(t, e.Store.Events, usecase.EventPaymentApproved)
		assert.Contains(t, e.Store.Events, usecase.EventPlanChanged)
		assert.True(t, e.Store.Flags["user-1"][domain.FeatureQuotes], "the entitlements follow the plan")
	})

	t.Run("a reviewed request is final", func(t *testing.T) {
		_, err := e.Payments.Approve(ctx, "admin", view.Request.ID, "again")
		requireReason(t, err, domain.ReasonRequestNotOpen)

		_, err = e.Payments.Cancel(ctx, "user-1", view.Request.ID)
		requireReason(t, err, domain.ReasonRequestNotOpen)

		_, err = e.Payments.Submit(ctx, "user-1", view.Request.ID, declare(599000))
		requireReason(t, err, domain.ReasonRequestNotOpen)
	})
}

func TestProrationBecomesCredit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	pay := func(planID string, kind domain.PaymentKind, amount int64) *domain.Subscription {
		t.Helper()

		v, err := e.Payments.Start(ctx, "user-1", planID, kind)
		require.NoError(t, err)

		_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, declare(amount))
		require.NoError(t, err)

		sub, err := e.Payments.Approve(ctx, "admin", v.Request.ID, "ok")
		require.NoError(t, err)

		return sub
	}

	pay("p-pro", domain.PaymentChange, 599000)

	// Half a year in, the yearly plan is swapped for the cheap monthly one:
	// the unused half pays for it and the rest stays as credit.
	e.Now = e.Now.AddDate(0, 6, 0)

	v, err := e.Payments.Start(ctx, "user-1", "p-starter", domain.PaymentChange)
	require.NoError(t, err)
	assert.Positive(t, v.Request.UnusedCredit)
	assert.Zero(t, v.Request.AmountDue)

	sub := pay("p-starter", domain.PaymentChange, 0)
	assert.Equal(t, "p-starter", sub.PlanID)
	assert.Equal(t, v.Request.UnusedCredit-299000, sub.CreditBalance)

	entries, err := e.Payments.ListCredit(ctx, "user-1")
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	t.Run("an admin corrects the balance", func(t *testing.T) {
		entry, err := e.Payments.AdjustCredit(ctx, "admin", sub.ID, -1000, "typo")
		require.NoError(t, err)
		assert.Equal(t, int64(-1000), entry.Amount)

		entry, err = e.Payments.AdjustCredit(ctx, "admin", sub.ID, -sub.CreditBalance*2, "too much")
		require.NoError(t, err)
		assert.Zero(t, entry.BalanceAfter, "the balance never goes below zero")

		_, err = e.Payments.AdjustCredit(ctx, "admin", sub.ID, -5, "nothing left")
		require.Error(t, err)

		_, err = e.Payments.AdjustCredit(ctx, "admin", sub.ID, 100, " ")
		require.Error(t, err, "a note is required")
	})
}

func TestRejectAndCancel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	v, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
	require.NoError(t, err)

	_, err = e.Payments.Reject(ctx, "admin", v.Request.ID, "no money received")
	requireReason(t, err, domain.ReasonRequestNotOpen)

	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, declare(599000))
	require.NoError(t, err)

	_, err = e.Payments.Reject(ctx, "admin", v.Request.ID, " ")
	require.Error(t, err, "a reason is required")

	rejected, err := e.Payments.Reject(ctx, "admin", v.Request.ID, "no money received")
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentRejected, rejected.Request.Status)
	assert.Contains(t, e.Store.Events, usecase.EventPaymentRejected)

	v, err = e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
	require.NoError(t, err, "a rejected request frees the owner to try again")

	cancelled, err := e.Payments.Cancel(ctx, "user-1", v.Request.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentCancelled, cancelled.Request.Status)

	open, err := e.Payments.OpenRequest(ctx, "user-1")
	require.NoError(t, err)
	assert.Nil(t, open)
}

func TestPlatformSettings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := licensetest.NewEnv()

	name, account := "Vietcombank", "0123456789"
	view, err := e.Payments.UpdateSettings(ctx, "admin", usecase.SettingsFields{BankName: &name, AccountNumber: &account})
	require.NoError(t, err)
	assert.Equal(t, "Vietcombank", view.Settings.BankName)
	assert.Contains(t, e.Store.Audits, "platform_payment_settings.updated")

	view, err = e.Payments.UploadQR(ctx, "admin", png, "image/png")
	require.NoError(t, err)
	assert.NotEmpty(t, view.QRURL)

	_, err = e.Payments.UploadQR(ctx, "admin", png, "text/plain")
	require.Error(t, err)

	instructions, qr, err := e.Payments.Instructions(ctx)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", instructions.AccountNumber)
	assert.NotEmpty(t, qr)

	t.Run("a quote carries the details as of its creation", func(t *testing.T) {
		require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "user-1"))

		v, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
		require.NoError(t, err)

		other := "Techcombank"
		_, err = e.Payments.UpdateSettings(ctx, "admin", usecase.SettingsFields{BankName: &other})
		require.NoError(t, err)

		again, err := e.Payments.GetMine(ctx, "user-1", v.Request.ID)
		require.NoError(t, err)
		assert.Equal(t, "Vietcombank", again.Request.Instructions.BankName)
	})
}

func TestReviewQueueAndReminders(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	e := trialOwner(t)

	v, err := e.Payments.Start(ctx, "user-1", "p-pro", domain.PaymentChange)
	require.NoError(t, err)
	_, err = e.Payments.Submit(ctx, "user-1", v.Request.ID, declare(599000))
	require.NoError(t, err)

	list, err := e.Payments.Review(ctx, usecase.ReviewFilter{Status: domain.PaymentSubmitted})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	detail, err := e.Payments.ReviewOne(ctx, v.Request.ID)
	require.NoError(t, err)
	assert.Equal(t, v.Request.ID, detail.Request.ID)

	// The reminder goes out once a day while a request waits.
	require.NoError(t, e.Expiry.Run(ctx))
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Equal(t, 1, countEvents(e, usecase.EventPaymentReviewReminder))

	e.Now = e.Now.Add(25 * time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Equal(t, 2, countEvents(e, usecase.EventPaymentReviewReminder))
}

func countEvents(e *licensetest.Env, name string) int {
	n := 0

	for _, ev := range e.Store.Events {
		if ev == name {
			n++
		}
	}

	return n
}
