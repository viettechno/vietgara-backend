// Release 5.2: a plan change or renewal is a bank-transfer payment request
// that a platform admin approves. These scenarios cover its rules; the
// plain happy path is the fixture every other scenario switches plans with.
package license_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// newPlan creates a public plan with its own price and limits.
func newPlan(t *testing.T, price int64, maxGarages, maxStaff int32) *licensev1.Plan {
	t.Helper()

	code := fixture.UniquePlanCode(fmt.Sprintf("qa-pay-%d", price))
	admin := fixture.PlatformAdmin(t)

	plan, err := admin.API.CreatePlan(t.Context(), &licensev1.Plan{
		Code: code, Name: "QA " + code, Price: price, Currency: "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits:       &licensev1.PlanLimits{MaxGarages: maxGarages, MaxStaff: maxStaff, MaxTransactionsPerDay: 100},
		IsPublic:     true, IsActive: true,
	})
	require.NoError(t, err)
	fixture.RetirePlanAfterTest(t, admin, plan.GetId())

	return plan
}

// declare submits the transfer for request: the amount due (or amount),
// with the evidence unless nothing is due.
func declare(t *testing.T, owner *fixture.Actor, request *licensev1.PlanPaymentRequest, amount int64) *licensev1.PlanPaymentRequest {
	t.Helper()

	submit := &licensev1.SubmitPlanPaymentRequest{
		RequestId: request.GetId(), PayerName: "QA Payer", PayerBank: "QA Bank", PayerAccount: "000111", TransferredAmount: amount,
		BankTransactionRef: "FT" + request.GetReference(), PaidAt: timestamppb.Now(),
	}
	if request.GetAmountDue() > 0 {
		submit.Evidence = fixture.TransferEvidence()
	}

	submitted, err := owner.API.SubmitPlanPayment(t.Context(), submit)
	require.NoError(t, err)

	return submitted
}

// pay quotes plan, declares the amount due and has the admin approve it.
func pay(t *testing.T, owner *fixture.Actor, plan *licensev1.Plan, kind licensev1.PlanPaymentKind) (*licensev1.PlanPaymentRequest, *licensev1.Subscription) {
	t.Helper()

	request, err := owner.API.CreatePlanPaymentRequest(t.Context(), plan.GetId(), kind)
	require.NoError(t, err)

	declare(t, owner, request, request.GetAmountDue())

	sub, err := fixture.PlatformAdmin(t).API.ApprovePlanPayment(t.Context(), request.GetId(), "")
	require.NoError(t, err)

	return request, sub
}

// Scenario: E2E-LIC-013: a request is quoted at the plan price less the
// credit of the unused days (nothing for a trial); moving to a cheaper plan
// leaves the surplus as account credit that the next payment uses.
func TestPaymentRequestAmountAndCredit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-credit")
	small := newPlan(t, 100_000, 2, 5)
	big := newPlan(t, 300_000, 2, 5)

	first, err := owner.API.CreatePlanPaymentRequest(ctx, small.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.EqualValues(t, 100_000, first.GetQuotedAmount())
	assert.EqualValues(t, 0, first.GetCreditApplied(), "a trial was paid 0, so no credit")
	assert.EqualValues(t, 100_000, first.GetAmountDue())
	assert.EqualValues(t, 0, first.GetCreditSurplus())
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_AWAITING_PAYMENT, first.GetStatus())
	assert.NotEmpty(t, first.GetReference(), "the transfer reference")
	require.NotNil(t, first.GetInstructions())

	declare(t, owner, first, first.GetAmountDue())

	sub, err := fixture.PlatformAdmin(t).API.ApprovePlanPayment(ctx, first.GetId(), "")
	require.NoError(t, err)
	assert.Equal(t, small.GetId(), sub.GetPlan().GetId())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, sub.GetStatus())
	assert.EqualValues(t, 100_000, sub.GetPeriodPaidAmount(), "the amount paid for the period")
	assert.True(t, sub.GetCurrentPeriodEnd().AsTime().After(time.Now().Add(27*24*time.Hour)), "a month from approval")

	// The credit is the paid amount times the whole unused days over the
	// days of the period, rounded down. Right after approval the day that
	// is running does not count as unused.
	upgrade, err := owner.API.CreatePlanPaymentRequest(ctx, big.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.EqualValues(t, 300_000, upgrade.GetQuotedAmount())
	assert.Positive(t, upgrade.GetPeriodDays())
	assert.Equal(t, upgrade.GetPeriodDays()-1, upgrade.GetUnusedDays(), "only whole days that are left count")
	assert.Equal(t, 100_000*int64(upgrade.GetUnusedDays())/int64(upgrade.GetPeriodDays()), upgrade.GetCreditApplied(), "rounded down to whole dong")
	assert.Equal(t, upgrade.GetQuotedAmount()-upgrade.GetCreditApplied(), upgrade.GetAmountDue())

	renew, err := owner.API.CreatePlanPaymentRequest(ctx, small.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_RENEW)
	require.NoError(t, err, "a new request replaces the one still waiting for payment")
	assert.LessOrEqual(t, renew.GetCreditApplied(), renew.GetQuotedAmount(), "the credit never exceeds the price")
	assert.Equal(t, renew.GetQuotedAmount()-renew.GetCreditApplied(), renew.GetAmountDue())

	// The renewal replaced the upgrade quote: quote the upgrade again.
	upgrade, err = owner.API.CreatePlanPaymentRequest(ctx, big.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)

	upgraded := declareAndApprove(t, owner, upgrade)
	assert.Equal(t, big.GetId(), upgraded.GetPlan().GetId())

	// Downgrade: the credit of the big period covers the small plan and the
	// rest stays on the account.
	downgrade, err := owner.API.CreatePlanPaymentRequest(ctx, small.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.EqualValues(t, 100_000, downgrade.GetQuotedAmount())
	assert.EqualValues(t, 100_000, downgrade.GetCreditApplied(), "the credit covers the whole price")
	assert.EqualValues(t, 0, downgrade.GetAmountDue(), "nothing to transfer")
	assert.Positive(t, downgrade.GetCreditSurplus(), "the rest is kept as credit")

	submitted := declare(t, owner, downgrade, 0)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_SUBMITTED, submitted.GetStatus(), "no evidence is needed when nothing is due")

	after, err := fixture.PlatformAdmin(t).API.ApprovePlanPayment(ctx, downgrade.GetId(), "")
	require.NoError(t, err)
	assert.Equal(t, small.GetId(), after.GetPlan().GetId())
	assert.Equal(t, downgrade.GetCreditSurplus(), after.GetCreditBalance(), "the surplus is the new balance")

	ledger, err := owner.API.CreditLedger(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, ledger)
	// Newest first, in the order they were written even when one approval
	// writes several entries at the same instant: the first entry ends at the
	// balance, and every balance follows from the one below it.
	require.NotEmpty(t, ledger)
	assert.Equal(t, after.GetCreditBalance(), ledger[0].GetBalanceAfter(), "the newest entry ends at the balance")

	for i := range len(ledger) - 1 {
		assert.Equal(t, ledger[i+1].GetBalanceAfter()+ledger[i].GetAmount(), ledger[i].GetBalanceAfter(),
			"entry %d follows from the one written before it", i)
		assert.GreaterOrEqual(t, ledger[i].GetBalanceAfter(), int64(0), "the balance never goes below zero")
	}

	// The next payment uses the balance automatically.
	next, err := owner.API.CreatePlanPaymentRequest(ctx, big.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, next.GetCreditApplied(), after.GetCreditBalance(), "the balance is applied")
}

func declareAndApprove(t *testing.T, owner *fixture.Actor, request *licensev1.PlanPaymentRequest) *licensev1.Subscription {
	t.Helper()

	declare(t, owner, request, request.GetAmountDue())

	sub, err := fixture.PlatformAdmin(t).API.ApprovePlanPayment(t.Context(), request.GetId(), "")
	require.NoError(t, err)

	return sub
}

// Scenario: E2E-LIC-014: one request is open per owner: a waiting one is
// replaced by a new quote, a submitted one blocks, and a request can be
// cancelled until it is reviewed. Nobody sees another owner's request.
func TestOnePaymentRequestIsOpenPerOwner(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-open")
	other := fixture.NewVerifiedOwner(t, "lic-open-other")
	a := newPlan(t, 100_000, 2, 5)
	b := newPlan(t, 200_000, 2, 5)

	first, err := owner.API.CreatePlanPaymentRequest(ctx, a.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)

	second, err := owner.API.CreatePlanPaymentRequest(ctx, b.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.NotEqual(t, first.GetId(), second.GetId())
	assert.NotEqual(t, first.GetReference(), second.GetReference(), "a new quote has a new reference")

	replaced, err := owner.API.MyPaymentRequest(ctx, first.GetId())
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_CANCELLED, replaced.GetStatus())

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, second.GetId(), sub.GetOpenRequest().GetId())

	declare(t, owner, second, second.GetAmountDue())

	_, err = owner.API.CreatePlanPaymentRequest(ctx, a.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusConflict, "REQUEST_ALREADY_OPEN")

	_, err = other.API.MyPaymentRequest(ctx, second.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	_, err = other.API.CancelPlanPayment(ctx, second.GetId())
	require.Error(t, err, "another owner cannot cancel it")

	cancelled, err := owner.API.CancelPlanPayment(ctx, second.GetId())
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_CANCELLED, cancelled.GetStatus())

	_, err = owner.API.CancelPlanPayment(ctx, second.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "REQUEST_NOT_OPEN")

	third, err := owner.API.CreatePlanPaymentRequest(ctx, a.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err, "after a cancel the owner can quote again")

	declareAndApprove(t, owner, third)

	_, err = owner.API.CancelPlanPayment(ctx, third.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "REQUEST_NOT_OPEN")

	requests, err := owner.API.MyPaymentRequests(ctx)
	require.NoError(t, err)
	assert.Len(t, requests, 3, "all of the owner's requests are listed")
}

// Scenario: E2E-LIC-015: the trial plan and a plan that is off sale cannot
// be requested.
func TestTrialAndRetiredPlansCannotBeRequested(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "lic-trial-again")

	all, err := admin.API.ListAllPlans(ctx)
	require.NoError(t, err)

	trialID := ""

	for _, plan := range all {
		if plan.GetCode() == "trial" {
			trialID = plan.GetId()
		}
	}

	require.NotEmpty(t, trialID, "the seeded trial plan")

	_, err = owner.API.CreatePlanPaymentRequest(ctx, trialID, licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "TRIAL_NOT_AVAILABLE")

	plan := newPlan(t, 100_000, 1, 5)
	_, err = admin.API.UpdatePlan(ctx, plan.GetId(), &licensev1.Plan{IsActive: false}, "is_active")
	require.NoError(t, err)

	_, err = owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "PLAN_NOT_AVAILABLE")

	requests, err := owner.API.MyPaymentRequests(ctx)
	require.NoError(t, err)
	assert.Empty(t, requests, "nothing was created")
}

// Scenario: E2E-LIC-016: the admin rejects a request with a reason; the
// owner sees it, the plan does not change, and a new request can follow.
func TestRejectedRequestCanBeSubmittedAgain(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "lic-reject")
	plan := newPlan(t, 100_000, 1, 5)

	request, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)

	_, err = admin.API.RejectPlanPayment(ctx, request.GetId(), "Too early")
	fixture.RequireAPIError(t, err, http.StatusConflict, "REQUEST_NOT_OPEN")

	_, err = owner.API.SubmitPlanPayment(ctx, &licensev1.SubmitPlanPaymentRequest{
		RequestId: request.GetId(), PayerName: "QA Payer", TransferredAmount: request.GetAmountDue(), PaidAt: timestamppb.Now(),
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "EVIDENCE_REQUIRED")

	declare(t, owner, request, request.GetAmountDue())

	_, err = admin.API.RejectPlanPayment(ctx, request.GetId(), "")
	require.Error(t, err, "a reason is required")

	rejected, err := admin.API.RejectPlanPayment(ctx, request.GetId(), "Transfer not received")
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_REJECTED, rejected.GetStatus())

	seen, err := owner.API.MyPaymentRequest(ctx, request.GetId())
	require.NoError(t, err)
	assert.Equal(t, "Transfer not received", seen.GetRejectionReason())

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, sub.GetStatus(), "the plan did not change")
	assert.Nil(t, sub.GetOpenRequest(), "a rejected request does not block")

	_, err = admin.API.ApprovePlanPayment(ctx, request.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusConflict, "REQUEST_NOT_OPEN")

	again, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)

	approved := declareAndApprove(t, owner, again)
	assert.Equal(t, plan.GetId(), approved.GetPlan().GetId())
}

// Scenario: E2E-LIC-018: when the amount transferred differs from the
// quote, the admin sees it and must add a note to approve.
func TestAmountMismatchNeedsANote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "lic-mismatch")
	plan := newPlan(t, 100_000, 1, 5)

	request, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)

	submitted := declare(t, owner, request, 90_000)
	assert.True(t, submitted.GetAmountMismatch())

	_, err = admin.API.ApprovePlanPayment(ctx, request.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "AMOUNT_MISMATCH_NOTE_REQUIRED")

	sub, err := admin.API.ApprovePlanPayment(ctx, request.GetId(), "Customer paid 10,000 short, will top up")
	require.NoError(t, err)
	assert.Equal(t, plan.GetId(), sub.GetPlan().GetId())
	assert.EqualValues(t, 100_000, sub.GetPeriodPaidAmount(), "the period records the quoted amount")
}

// Scenario: E2E-LIC-019: the plan's limits are checked when the request is
// made and again when the admin approves it.
func TestLimitsAreCheckedAgainAtApproval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "lic-limit-approval")
	roomy := newPlan(t, 200_000, 3, 5)
	tight := newPlan(t, 100_000, 1, 5)

	pay(t, owner, roomy, licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)

	_, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "QA Limit One"})
	require.NoError(t, err)

	request, err := owner.API.CreatePlanPaymentRequest(ctx, tight.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err, "one garage fits the tight plan")

	declare(t, owner, request, request.GetAmountDue())

	_, err = owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "QA Limit Two"})
	require.NoError(t, err, "the roomy plan still allows a second garage")

	_, err = admin.API.ApprovePlanPayment(ctx, request.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")

	still, err := owner.API.MyPaymentRequest(ctx, request.GetId())
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_SUBMITTED, still.GetStatus(), "the request keeps waiting")

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, roomy.GetId(), sub.GetPlan().GetId(), "the plan did not change")

	// The limits are checked at every quote as well (E2E-LIC-003).
	_, err = owner.API.CancelPlanPayment(ctx, request.GetId())
	require.NoError(t, err)

	_, err = owner.API.CreatePlanPaymentRequest(ctx, tight.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")
}

// Scenario: E2E-LIC-020: the platform's payment details are edited by an
// admin only, a quote keeps the instructions it was made with, and the
// credit of a subscription can be corrected but never below zero.
func TestPaymentSettingsAndCreditAdjustments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "lic-settings")
	plan := newPlan(t, 100_000, 1, 5)

	holder := fmt.Sprintf("QA Holder %d", time.Now().UnixNano()%1_000_000)

	_, err := owner.API.PlatformPaymentSettings(ctx)
	require.Error(t, err, "an owner cannot read the admin view")

	_, err = owner.API.UpdatePlatformPaymentSettings(ctx, &licensev1.PlatformPaymentSettings{AccountHolder: holder}, "account_holder")
	require.Error(t, err, "an owner cannot change the details")

	saved, err := admin.API.UpdatePlatformPaymentSettings(ctx, &licensev1.PlatformPaymentSettings{
		BankName: "QA Bank", AccountNumber: "0123456789", AccountHolder: holder,
	}, "bank_name,account_number,account_holder")
	require.NoError(t, err)
	assert.Equal(t, holder, saved.GetAccountHolder())

	request, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	assert.Equal(t, holder, request.GetInstructions().GetAccountHolder(), "the owner is shown what the admin saved")

	_, err = admin.API.UpdatePlatformPaymentSettings(ctx, &licensev1.PlatformPaymentSettings{AccountHolder: holder + " 2"}, "account_holder")
	require.NoError(t, err)

	kept, err := owner.API.MyPaymentRequest(ctx, request.GetId())
	require.NoError(t, err)
	assert.Equal(t, holder, kept.GetInstructions().GetAccountHolder(), "a quote keeps the instructions it started with")

	// Credit adjustments.
	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)

	_, err = owner.API.AdjustCredit(ctx, sub.GetId(), 1000, "no")
	require.Error(t, err, "owners do not adjust their own credit")

	entry, err := admin.API.AdjustCredit(ctx, sub.GetId(), 10_000, "Goodwill")
	require.NoError(t, err)
	assert.EqualValues(t, 10_000, entry.GetBalanceAfter())
	assert.Equal(t, licensev1.CreditEntryKind_CREDIT_ENTRY_KIND_ADJUSTMENT, entry.GetKind())

	floor, err := admin.API.AdjustCredit(ctx, sub.GetId(), -5_000_000_000, "Clear it")
	require.NoError(t, err)
	assert.EqualValues(t, 0, floor.GetBalanceAfter(), "the balance never goes below zero")

	ledger, err := owner.API.CreditLedger(ctx)
	require.NoError(t, err)
	assert.Len(t, ledger, 2)
	assert.Equal(t, "Clear it", ledger[0].GetNote())
}
