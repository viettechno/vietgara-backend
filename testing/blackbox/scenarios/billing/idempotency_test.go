// Package billing_test covers Billing & Settlement end to end: a closed
// repair order is settled and paid, with the Idempotency-Key the API
// requires for both (API Specification Section 1.1).
package billing_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"google.golang.org/protobuf/proto"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

const total = 500000

// closedRepairOrder walks a new garage to a closed repair order with one
// labour line of total VND.
func closedRepairOrder(t *testing.T, api *client.Client) (garageID, orderID string) {
	t.Helper()

	ctx := t.Context()

	garage, err := api.CreateGarage(ctx, &tenantv1.Garage{Name: "Billing Garage"})
	require.NoError(t, err)

	customer, err := api.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{FullName: "Khách Thanh Toán", PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle, err := api.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-12345"})
	require.NoError(t, err)

	order, err := api.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: total,
		}},
	})
	require.NoError(t, err)
	require.Len(t, order.GetLines(), 1)

	_, err = api.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = api.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)

	return garage.GetId(), order.GetId()
}

func cash(amount int64) *billingv1.RecordSettlementPaymentRequest {
	return &billingv1.RecordSettlementPaymentRequest{
		Amount: amount, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
	}
}

// TestSettlementAndPaymentsAreIdempotent: a retried settlement or payment
// is answered from the first attempt, never created twice.
//
// Scenario: E2E-BIL-006 (partial): partial payments, vouchers, voids and
// receipt PDFs are later batches.
func TestSettlementAndPaymentsAreIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "billing")
	garageID, orderID := closedRepairOrder(t, owner.API)

	var settlement *billingv1.Settlement

	t.Run("creating a settlement needs a key", func(t *testing.T) {
		_, err := owner.API.CreateSettlement(ctx, garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED")
	})

	t.Run("a retried settlement returns the first one", func(t *testing.T) {
		withKey := owner.API.WithIdempotencyKey(uuid.NewString())

		var err error

		settlement, err = withKey.CreateSettlement(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.EqualValues(t, total, settlement.GetTotalAmount())

		again, err := withKey.CreateSettlement(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.True(t, proto.Equal(settlement, again), "the stored answer, not a second settlement")

		_, err = owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})

	t.Run("concurrent retries of one payment record it once", func(t *testing.T) {
		withKey := owner.API.WithIdempotencyKey(uuid.NewString())

		const attempts = 5

		answers := make([]*billingv1.Settlement, attempts)
		errs := make([]error, attempts)

		var wg sync.WaitGroup
		for i := range attempts {
			wg.Go(func() { answers[i], errs[i] = withKey.RecordPayment(ctx, garageID, settlement.GetId(), cash(100000)) })
		}

		wg.Wait()

		for i := range attempts {
			require.NoError(t, errs[i])
			assert.EqualValues(t, 100000, answers[i].GetPaidAmount(), "attempt %d", i)
			assert.True(t, proto.Equal(answers[0], answers[i]), "every attempt gets the same answer")
		}

		current, err := owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, garageID, settlement.GetId(), cash(100000))
		require.NoError(t, err)
		assert.EqualValues(t, 200000, current.GetPaidAmount(), "only one of the five was recorded")
		assert.Len(t, current.GetPayments(), 2)
	})

	t.Run("a key reused for another payment is refused", func(t *testing.T) {
		withKey := owner.API.WithIdempotencyKey(uuid.NewString())

		paid, err := withKey.RecordPayment(ctx, garageID, settlement.GetId(), cash(100000))
		require.NoError(t, err)

		_, err = withKey.RecordPayment(ctx, garageID, settlement.GetId(), cash(200000))
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED")

		final, err := owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, garageID, settlement.GetId(), cash(200000))
		require.NoError(t, err)
		assert.Equal(t, paid.GetPaidAmount()+200000, final.GetPaidAmount())
		assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, final.GetStatus())
	})

	t.Run("a failed payment can be retried with its key", func(t *testing.T) {
		withKey := owner.API.WithIdempotencyKey(uuid.NewString())

		_, err := withKey.RecordPayment(ctx, garageID, settlement.GetId(), cash(1))
		require.Error(t, err, "nothing is due any more")

		_, err = withKey.RecordPayment(ctx, garageID, settlement.GetId(), cash(1))
		require.Error(t, err)
		assert.NotEqual(t, "IDEMPOTENCY_KEY_REUSED", client.AsAPIError(err).Code, "a failure stores nothing to replay")
	})
}
