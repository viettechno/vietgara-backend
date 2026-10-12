// Settlement lifecycle end to end: creation from a closed order,
// guards, partial payments to PAID, numbered vouchers, void with
// restore, and the outstanding filter. The idempotency-key behavior
// lives in idempotency_test.go; receipt and voucher PDFs live in the
// reporting package; the dashboard revenue leg is covered there too.
package billing_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// settle returns a fresh pending settlement of total VND (see
// idempotency_test.go's closedRepairOrder).
func settle(t *testing.T, label string) (*fixture.Actor, string, *billingv1.Settlement) {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, label)
	garageID, orderID := closedRepairOrder(t, owner.API)

	settlement, err := owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
	require.NoError(t, err)

	return owner, garageID, settlement
}

// openRepairOrder creates a garage with an order that is still open.
func openRepairOrder(t *testing.T, api *client.Client) (garageID, orderID string) {
	t.Helper()

	ctx := t.Context()

	garage, err := api.CreateGarage(ctx, &tenantv1.Garage{Name: "Billing Open Garage"})
	require.NoError(t, err)

	customer, err := api.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{FullName: "Khách Mở", PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle, err := api.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51B-99999"})
	require.NoError(t, err)

	order, err := api.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Check-up", Quantity: "1", UnitPrice: total,
		}},
	})
	require.NoError(t, err)

	return garage.GetId(), order.GetId()
}

// pay records a cash payment with a fresh idempotency key.
func pay(t *testing.T, owner *fixture.Actor, garageID, settlementID string, amount int64) *billingv1.Settlement {
	t.Helper()

	updated, err := owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(t.Context(), garageID, settlementID, cash(amount))
	require.NoError(t, err)

	return updated
}

// Scenario: E2E-BIL-001 (partial): a settlement is created from a
// closed repair order — the API substance (number, Pending, total,
// customer and plate); the picker is web-E2E.
func TestCreateSettlement(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, settlement := settle(t, "bil-create")

	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING, settlement.GetStatus())
	assert.EqualValues(t, total, settlement.GetTotalAmount())
	assert.EqualValues(t, 0, settlement.GetPaidAmount())
	assert.NotEmpty(t, settlement.GetNumber())
	assert.NotEmpty(t, settlement.GetCustomerName())
	assert.NotEmpty(t, settlement.GetVehicleLicensePlate())
	assert.Empty(t, settlement.GetPayments())

	got, err := owner.API.GetSettlement(ctx, garageID, settlement.GetId())
	require.NoError(t, err)
	assert.Equal(t, settlement.GetId(), got.GetId())

	listed, err := owner.API.ListSettlements(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, listed)

	outstanding, err := owner.API.ListOutstandingSettlements(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, outstanding)
}

// Scenario: E2E-BIL-002 (partial): a repair order settles only once,
// and an open order cannot be settled — the API guards.
func TestSettlementGuards(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bil-guards")
	garageID, orderID := closedRepairOrder(t, owner.API)

	_, err := owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
	require.NoError(t, err)

	// A second settlement for the same order is refused.
	_, err = owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// An order still in progress cannot be settled.
	openGarageID, openOrderID := openRepairOrder(t, owner.API)
	_, err = owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, openGarageID, openOrderID)
	fixture.RequireAPIError(t, err, http.StatusPreconditionFailed, "")
}

// Scenario: E2E-BIL-003 (partial): a partial payment, then the rest to
// PAID, then a refused overpayment — asserted at API level; the payment
// form is web-E2E.
func TestPartialPaymentsToPaid(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, settlement := settle(t, "bil-partial")

	partial := pay(t, owner, garageID, settlement.GetId(), 200000)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID, partial.GetStatus())
	assert.EqualValues(t, 200000, partial.GetPaidAmount())
	require.Len(t, partial.GetPayments(), 1)

	paid := pay(t, owner, garageID, settlement.GetId(), 300000)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, paid.GetStatus())
	assert.EqualValues(t, total, paid.GetPaidAmount())
	require.Len(t, paid.GetPayments(), 2)

	// Nothing outstanding: a third payment is refused.
	_, err := owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, garageID, settlement.GetId(), cash(1000))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// The paid settlement leaves the outstanding view.
	outstanding, err := owner.API.ListOutstandingSettlements(ctx, garageID)
	require.NoError(t, err)
	assert.Empty(t, outstanding)
}

// Scenario: E2E-BIL-004 (partial): each payment gets a numbered voucher
// — asserted at API level; the voucher PDF layout is web-E2E (raw bytes
// are covered in the reporting package).
func TestNumberedVouchers(t *testing.T) {
	t.Parallel()

	owner, garageID, settlement := settle(t, "bil-voucher")

	pay(t, owner, garageID, settlement.GetId(), 200000)
	paid := pay(t, owner, garageID, settlement.GetId(), 300000)

	require.Len(t, paid.GetPayments(), 2)
	assert.NotEmpty(t, paid.GetPayments()[0].GetVoucherNumber())
	assert.NotEmpty(t, paid.GetPayments()[1].GetVoucherNumber())
	assert.NotEqual(t, paid.GetPayments()[0].GetVoucherNumber(), paid.GetPayments()[1].GetVoucherNumber())
	assert.NotEmpty(t, paid.GetPayments()[0].GetCollectorName())
}

// Scenario: E2E-BIL-005 (partial): voiding a payment restores the
// outstanding amount while the voucher stays on file — the API
// substance; the audit log view is web-E2E.
func TestVoidPaymentRestoresOutstanding(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID, settlement := settle(t, "bil-void")

	pay(t, owner, garageID, settlement.GetId(), 200000)
	paid := pay(t, owner, garageID, settlement.GetId(), 300000)
	require.Len(t, paid.GetPayments(), 2)

	// Voiding without a reason is refused.
	_, err := owner.API.VoidSettlementPayment(ctx, garageID, settlement.GetId(), paid.GetPayments()[1].GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	voided, err := owner.API.VoidSettlementPayment(ctx, garageID, settlement.GetId(), paid.GetPayments()[1].GetId(), "Entered twice")
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID, voided.GetStatus())
	assert.EqualValues(t, 200000, voided.GetPaidAmount())
	require.Len(t, voided.GetPayments(), 2)
	assert.True(t, voided.GetPayments()[1].GetVoided())
	assert.Equal(t, "Entered twice", voided.GetPayments()[1].GetVoidReason())
	assert.NotNil(t, voided.GetPayments()[1].GetVoidedAt())

	// The settlement re-enters the outstanding view.
	outstanding, err := owner.API.ListOutstandingSettlements(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, outstanding)
}

// Scenario: E2E-BIL-008 (partial): the outstanding filter follows the
// settlement — asserted at API level; the dashboard figure is covered
// in the reporting package.
func TestOutstandingFilter(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bil-outfilter")
	garageID, orderID := closedRepairOrder(t, owner.API)

	pending, err := owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
	require.NoError(t, err)

	outstanding, err := owner.API.ListOutstandingSettlements(ctx, garageID)
	require.NoError(t, err)
	require.Len(t, outstanding, 1)
	assert.Equal(t, pending.GetId(), outstanding[0].GetId())
}
