// Scenario: E2E-BIL-009
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/08_Settlement.md
package billing_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestBillingPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "bil-perms-owner", identityv1.Locale_LOCALE_EN)
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Billing Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create a closed repair order to settle
	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Khách Billing", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51B-11111"})
	require.NoError(t, err)

	order, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Service",
			Quantity:    "1",
			UnitPrice:   1000000,
		}},
	})
	require.NoError(t, err)

	// Complete the line before closing
	lines, err := owner.API.GetRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)

	line := lines.GetLines()[0]
	line.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), line.GetId(), line, "status")
	require.NoError(t, err)

	closed, err := owner.API.CloseRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())

	// --- Test 1: Staff with NO billing permission (Technician) cannot access settlements ---
	tech := fixture.NewVerifiedOwner(t, "bil-tech")
	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)

	// Find Technician group (no billing permission)
	var techGroup *tenantv1.StaffGroup

	for _, g := range groups {
		if g.GetName() == "Technician" {
			techGroup = g
			break
		}
	}

	require.NotNil(t, techGroup, "Technician group should exist")

	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{techGroup.GetId()})
	require.NoError(t, err)

	techInvites, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, techInvites)
	_, err = tech.API.AcceptInvitation(ctx, techInvites[0].GetId())
	require.NoError(t, err)

	// Technician tries to create settlement - should fail
	_, err = tech.API.WithIdempotencyKey("test-tech-key").CreateSettlement(ctx, garageID, closed.GetId())
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// Technician tries to list settlements - should fail
	_, err = tech.API.ListSettlements(ctx, garageID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// Technician tries to get settlement - should fail
	_, err = tech.API.GetSettlement(ctx, garageID, "")
	require.Error(t, err)

	// --- Test 2: Owner (billing write) can create, list, get, pay, void ---
	// Create settlement
	settlement, err := owner.API.WithIdempotencyKey("test-key-1").CreateSettlement(ctx, garageID, closed.GetId())
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING, settlement.GetStatus())

	// List settlements
	listed, err := owner.API.ListSettlements(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, listed)

	// Get settlement
	got, err := owner.API.GetSettlement(ctx, garageID, settlement.GetId())
	require.NoError(t, err)
	assert.Equal(t, settlement.GetId(), got.GetId())

	// Record payment
	paid, err := owner.API.WithIdempotencyKey("pay-key-1").RecordPayment(ctx, garageID, settlement.GetId(), &billingv1.RecordSettlementPaymentRequest{Amount: 1000000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, paid.GetStatus())

	// Void payment
	voided, err := owner.API.VoidSettlementPayment(ctx, garageID, settlement.GetId(), paid.GetPayments()[0].GetId(), "Test void")
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING, voided.GetStatus())

	// --- Test 3: Staff with billing write can do everything owner can ---
	// (would need a custom role with billing write - skip for now as baseline may not have it)
}
