// Scenario: E2E-LIC-024
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

// Credit is used oldest first and lapses one year after it was earned; a
// quote counts only credit that has not lapsed, and an approval refuses the
// credit of a quote that lapsed since (FR-LIC-08, FR-LIC-09).
func TestCreditIsUsedOldestFirstAndLapses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-lots")
	admin := fixture.PlatformAdmin(t)
	plan := newPlan(t, 600_000, 2, 5)

	_, sub := pay(t, owner, plan, licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)

	lotA, err := admin.API.AdjustCredit(ctx, sub.GetId(), 500_000, "lot A")
	require.NoError(t, err)
	assert.EqualValues(t, 500_000, lotA.GetRemaining())
	assert.WithinDuration(t, time.Now().AddDate(1, 0, 0), lotA.GetExpiresAt().AsTime(), time.Hour, "a year after it was earned")

	_, err = admin.API.AdjustCredit(ctx, sub.GetId(), 300_000, "lot B")
	require.NoError(t, err)

	mine, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 800_000, mine.GetCreditBalance())
	assert.NotNil(t, mine.GetNextCreditExpiry())

	// A renewal of the 600,000 plan takes all of lot A and 100,000 of lot B.
	renew, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_RENEW)
	require.NoError(t, err)
	assert.EqualValues(t, 600_000, renew.GetCreditApplied())
	assert.Zero(t, renew.GetAmountDue(), "the credit pays all of it")

	declare(t, owner, renew, 0)

	_, err = admin.API.ApprovePlanPayment(ctx, renew.GetId(), "")
	require.NoError(t, err)

	mine, err = owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 200_000, mine.GetCreditBalance())

	ledger, err := owner.API.CreditLedger(ctx)
	require.NoError(t, err)

	remaining := map[string]int64{}

	for _, e := range ledger {
		if e.GetKind() == licensev1.CreditEntryKind_CREDIT_ENTRY_KIND_ADJUSTMENT {
			remaining[e.GetNote()] = e.GetRemaining()
		}
	}

	assert.Equal(t, map[string]int64{"lot A": 0, "lot B": 200_000}, remaining, "the older lot is used first")

	// What is left lapses: the job writes the expired entry.
	timeshift.ExpireCredit(t, owner.Email)

	require.Eventually(t, func() bool {
		entries, listErr := owner.API.CreditLedger(ctx)
		require.NoError(t, listErr)

		return len(entries) > 0 && entries[0].GetKind() == licensev1.CreditEntryKind_CREDIT_ENTRY_KIND_EXPIRED
	}, expiryWait, time.Second, "an expired entry")

	ledger, err = owner.API.CreditLedger(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, -200_000, ledger[0].GetAmount())

	mine, err = owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Zero(t, mine.GetCreditBalance())
	assert.Nil(t, mine.GetNextCreditExpiry())

	// A quote made with credit is refused at approval once the credit lapsed.
	_, err = admin.API.AdjustCredit(ctx, sub.GetId(), 400_000, "lot C")
	require.NoError(t, err)

	quoted, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_RENEW)
	require.NoError(t, err)
	assert.EqualValues(t, 400_000, quoted.GetCreditApplied())

	declare(t, owner, quoted, quoted.GetAmountDue())
	timeshift.ExpireCredit(t, owner.Email)

	_, err = admin.API.ApprovePlanPayment(ctx, quoted.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "CREDIT_EXPIRED")

	// Cancel and quote again: nothing lapsed is applied.
	_, err = owner.API.CancelPlanPayment(ctx, quoted.GetId())
	require.NoError(t, err)

	again, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_RENEW)
	require.NoError(t, err)
	assert.Zero(t, again.GetCreditApplied())
	assert.EqualValues(t, 600_000, again.GetAmountDue())
}
