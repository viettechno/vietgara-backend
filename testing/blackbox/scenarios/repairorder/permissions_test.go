// Scenario: E2E-REP-012
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/06_Repair_Order.md
package repairorder_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestRepairOrderPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "rep-perms-owner")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox RO Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Khách RO Perms", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51R-44444"})
	require.NoError(t, err)

	// Create repair order with incomplete line
	order, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Brake repair", Quantity: "1", UnitPrice: "200000",
		}},
	})
	require.NoError(t, err)
	orderID := order.GetId()

	// --- Test 1: Staff with NO repair-order access cannot access ---
	noPerm := fixture.NewVerifiedOwner(t, "rep-no-perm")
	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, noPerm.Email)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	noPermInvites, err := noPerm.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, noPermInvites)
	_, err = noPerm.API.AcceptInvitation(ctx, noPermInvites[0].GetId())
	require.NoError(t, err)

	// No perm staff tries to list repair orders - should fail
	_, err = noPerm.API.ListRepairOrders(ctx, garageID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to get repair order - should fail
	_, err = noPerm.API.GetRepairOrder(ctx, garageID, orderID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to update line - should fail
	_, err = noPerm.API.UpdateRepairOrderLine(ctx, garageID, orderID, "", &repairorderv1.RepairOrderLine{})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to close - should fail
	_, err = noPerm.API.CloseRepairOrder(ctx, garageID, orderID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// --- Test 2: Staff with VIEW ONLY (Inventory Staff role) can read but not write ---
	viewer := fixture.NewVerifiedOwner(t, "rep-viewer")
	cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, viewer.Email)
	require.NoError(t, err)

	// Find Inventory Staff group (view repair orders, no write)
	var viewerGroup *tenantv1.StaffGroup
	for _, g := range groups {
		if g.GetName() == "Inventory Staff" || g.GetRole().GetName() == "Inventory Staff" {
			viewerGroup = g
			break
		}
	}
	if viewerGroup != nil {
		_, err = owner.API.CreateInvitation(ctx, garageID, cand2.GetUserId(), []string{viewerGroup.GetId()})
		require.NoError(t, err)

		viewerInvites, err := viewer.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, viewerInvites)
		_, err = viewer.API.AcceptInvitation(ctx, viewerInvites[0].GetId())
		require.NoError(t, err)

		// Viewer can list repair orders
		viewerROs, err := viewer.API.ListRepairOrders(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, viewerROs)

		// Viewer can get repair order
		got, err := viewer.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, orderID, got.GetId())

		// Viewer CANNOT update line status
		_, err = viewer.API.UpdateRepairOrderLine(ctx, garageID, orderID, "", &repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Viewer CANNOT assign technician
		_, err = viewer.API.UpdateRepairOrderLine(ctx, garageID, orderID, "", &repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{""}})
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Viewer CANNOT close
		_, err = viewer.API.CloseRepairOrder(ctx, garageID, orderID)
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	}

	// --- Test 3: Staff with READ/WRITE (Technician role) can fully manage ---
	tech := fixture.NewVerifiedOwner(t, "rep-tech-write")
	cand3, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	var techGroup *tenantv1.StaffGroup
	for _, g := range groups {
		if g.GetName() == "Technician" || g.GetRole().GetName() == "Technician" {
			techGroup = g
			break
		}
	}
	if techGroup != nil {
		_, err = owner.API.CreateInvitation(ctx, garageID, cand3.GetUserId(), []string{techGroup.GetId()})
		require.NoError(t, err)

		techInvites, err := tech.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, techInvites)
		_, err = tech.API.AcceptInvitation(ctx, techInvites[0].GetId())
		require.NoError(t, err)

		// Technician can list, get
		techROs, err := tech.API.ListRepairOrders(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, techROs)

		got, err := tech.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, orderID, got.GetId())

		// Technician can update line status
		lines, err := tech.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		techLine := lines.GetLines()[0]
		techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
		updated, err := tech.API.UpdateRepairOrderLine(ctx, garageID, orderID, techLine.GetId(), techLine)
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS, updated.GetLines()[0].GetStatus())

		// Technician can assign self
		techLine.AssignedTechnicianIds = []string{tech.Session.GetUser().GetId()}
		techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
		updated, err = tech.API.UpdateRepairOrderLine(ctx, garageID, orderID, techLine.GetId(), techLine)
		require.NoError(t, err)
		assert.Len(t, updated.GetLines()[0].GetAssignedTechnicianIds(), 1)
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, updated.GetLines()[0].GetStatus())

		// Technician CAN close once all lines completed
		closed, err := tech.API.CloseRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())
	}

	// --- Test 4: Owner/Manager (full access) same as technician ---
	// Owner can also close a different order
	order2, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Owner Close", Quantity: "1", UnitPrice: "100000",
		}},
	})
	require.NoError(t, err)

	lines2, err := owner.API.GetRepairOrder(ctx, garageID, order2.GetId())
	require.NoError(t, err)
	ownerLine := lines2.GetLines()[0]
	ownerLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	_, err = owner.API.UpdateRepairOrderLine(ctx, garageID, order2.GetId(), ownerLine.GetId(), ownerLine)
	require.NoError(t, err)

	closed2, err := owner.API.CloseRepairOrder(ctx, garageID, order2.GetId())
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed2.GetStatus())

	// A closed order cannot be edited further
	_, err = owner.API.UpdateRepairOrderLine(ctx, garageID, order2.GetId(), ownerLine.GetId(), &repairorderv1.RepairOrderLine{Description: "Try edit"})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Closing again is refused
	_, err = owner.API.CloseRepairOrder(ctx, garageID, order2.GetId())
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}