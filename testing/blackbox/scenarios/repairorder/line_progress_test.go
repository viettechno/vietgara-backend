// Scenario: E2E-REP-006
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/06_Repair_Order.md
package repairorder_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func createLineProgressTestEnv(t *testing.T) (context.Context, *fixture.Actor, string, *customerv1.Customer, *customerv1.Vehicle, string, []*tenantv1.StaffGroup) {
	t.Helper()

	owner := fixture.NewVerifiedOwner(t, "rep-line-progress")
	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox Line Progress"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(t.Context(), garageID, &customerv1.Customer{FullName: "Khách Line Progress", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(t.Context(), garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51R-33333"})
	require.NoError(t, err)

	// Create repair order with 2 lines
	order, err := owner.API.CreateRepairOrder(t.Context(), garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Line A",
			Quantity:    "1",
			UnitPrice:   100000,
		}, {
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Line B",
			Quantity:    "1",
			UnitPrice:   100000,
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, order.GetStatus())

	orderID := order.GetId()

	groups, err := owner.API.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	return t.Context(), owner, garageID, customer, vehicle, orderID, groups
}

func TestLineProgressDrivesOrderStatus(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _, orderID, _ := createLineProgressTestEnv(t)

	// Get line IDs
	lines, err := owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	require.Len(t, lines.GetLines(), 2)
	lineA := lines.GetLines()[0]
	lineB := lines.GetLines()[1]

	// Step 1: Both Not started -> To Do (Open)
	ro, err := owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, ro.GetStatus())

	// Step 2: Line A -> In progress -> In Progress
	lineA.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineA.GetId(), lineA, "status")
	require.NoError(t, err)

	ro, err = owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, ro.GetStatus())

	// Step 3: Line A -> Completed, Line B -> In progress -> In Progress
	lineA.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineA.GetId(), lineA, "status")
	require.NoError(t, err)

	lineB.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineB.GetId(), lineB, "status")
	require.NoError(t, err)

	ro, err = owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, ro.GetStatus())

	// Step 4: Both Completed -> Completed
	lineB.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineB.GetId(), lineB, "status")
	require.NoError(t, err)

	ro, err = owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, ro.GetStatus())

	// Verify each save updates only that line; other lines untouched
	ro, err = owner.API.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)
	require.Len(t, ro.GetLines(), 2)

	for _, l := range ro.GetLines() {
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, l.GetStatus())
	}
}

func TestTechnicianAssignmentPreservedOnStatusChange(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, vehicle, _, groups := createLineProgressTestEnv(t)

	// Technician checkboxes for that line are unaffected by status-only save
	// (status and technicians are edited and saved together from the same row,
	// but changing one does not silently clear the other)
	// Assign a technician to line A before completing it
	tech := fixture.NewVerifiedOwner(t, "rep-tech-assign")
	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	techGroup := groups[0]
	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{techGroup.GetId()})
	require.NoError(t, err)

	techInvites, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, techInvites)
	_, err = tech.API.AcceptInvitation(ctx, techInvites[0].GetId())
	require.NoError(t, err)

	// Create a new order for this test
	order2, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Tech Line",
			Quantity:    "1",
			UnitPrice:   100000,
		}},
	})
	require.NoError(t, err)

	lines2, err := owner.API.GetRepairOrder(ctx, garageID, order2.GetId())
	require.NoError(t, err)

	techLine := lines2.GetLines()[0]

	// Assign technician
	techLine.AssignedTechnicianIds = []string{tech.Session.GetUser().GetId()}
	techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, order2.GetId(), techLine.GetId(), techLine, "status", "assigned_technician_ids")
	require.NoError(t, err)

	// Now change status only (should not clear technician)
	techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	techLine.AssignedTechnicianIds = nil // empty in request = don't change
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, order2.GetId(), techLine.GetId(), techLine, "status")
	require.NoError(t, err)

	// Verify technician assignment preserved
	ro2, err := owner.API.GetRepairOrder(ctx, garageID, order2.GetId())
	require.NoError(t, err)
	assert.Len(t, ro2.GetLines()[0].GetAssignedTechnicianIds(), 1)
	assert.Equal(t, tech.Session.GetUser().GetId(), ro2.GetLines()[0].GetAssignedTechnicianIds()[0])
	assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, ro2.GetLines()[0].GetStatus())
}
