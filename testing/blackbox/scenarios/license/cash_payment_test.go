// Scenario: E2E-LIC-025
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// The owner pays the amount due in cash: no bank details or evidence are
// needed and the admin confirms the receipt by approving (FR-LIC-07, FR-LIC-08).
func TestCashPaymentIsApproved(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-cash")
	plan := newPlan(t, 300_000, 2, 5)

	request, err := owner.API.CreatePlanPaymentRequest(ctx, plan.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.NoError(t, err)
	require.Positive(t, request.GetAmountDue())

	// A bank transfer still needs its evidence.
	_, err = owner.API.SubmitPlanPayment(ctx, &licensev1.SubmitPlanPaymentRequest{
		RequestId: request.GetId(), PayerName: "QA Payer", TransferredAmount: request.GetAmountDue(), PaidAt: timestamppb.Now(),
	})
	require.Error(t, err)

	submitted, err := owner.API.SubmitPlanPayment(ctx, &licensev1.SubmitPlanPaymentRequest{
		RequestId: request.GetId(), PayerName: "QA Payer", TransferredAmount: request.GetAmountDue(), PaidAt: timestamppb.Now(),
		PaymentMethod: licensev1.PaymentMethod_PAYMENT_METHOD_CASH,
	})
	require.NoError(t, err)
	assert.Equal(t, licensev1.PaymentMethod_PAYMENT_METHOD_CASH, submitted.GetPaymentMethod())
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_SUBMITTED, submitted.GetStatus())
	assert.Empty(t, submitted.GetPayerBank())
	assert.Empty(t, submitted.GetEvidenceUrl())

	sub, err := fixture.PlatformAdmin(t).API.ApprovePlanPayment(ctx, request.GetId(), "Received in cash")
	require.NoError(t, err)
	assert.Equal(t, plan.GetId(), sub.GetPlan().GetId())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, sub.GetStatus())
}
