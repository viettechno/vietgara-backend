// Scenario: E2E-REP-012
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/06_Repair_Order.md
package repairorder_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func createRepairOrderPermTestEnv(t *testing.T) (context.Context, *fixture.Actor, string, *customerv1.Customer, *customerv1.Vehicle, string, []*tenantv1.StaffGroup) {
	t.Helper()

	owner := fixture.NewVerifiedOwnerWithLocale(t, "rep-perms-owner", identityv1.Locale_LOCALE_EN)
	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox RO Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(t.Context(), garageID, &customerv1.Customer{FullName: "Khách RO Perms", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(t.Context(), garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51R-44444"})
	require.NoError(t, err)

	// Create repair order with incomplete line
	order, err := owner.API.CreateRepairOrder(t.Context(), garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Brake repair",
			Quantity:    "1",
			UnitPrice:   200000,
		}},
	})
	require.NoError(t, err)

	orderID := order.GetId()

	groups, err := owner.API.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	return t.Context(), owner, garageID, customer, vehicle, orderID, groups
}

func TestRepairOrderPermissionsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _, orderID, groups := createRepairOrderPermTestEnv(t)

	t.Run("technician has full repair order access", func(t *testing.T) {
		t.Parallel()
		tech := fixture.NewVerifiedOwner(t, "rep-tech-full")
		cand, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
		require.NoError(t, err)

		// Find Technician group (has RepairOrderRead + RepairOrderWrite)
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

		// Technician CAN list repair orders
		techROs, err := tech.API.ListRepairOrders(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, techROs)

		// Technician CAN get repair order
		got, err := tech.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, orderID, got.GetId())

		// Technician CAN update line status
		lines, err := tech.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)

		techLine := lines.GetLines()[0]
		techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
		updated, err := tech.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, techLine.GetId(), techLine, "status")
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS, updated.GetLines()[0].GetStatus())

		// Technician CAN assign self
		techLine.AssignedTechnicianIds = []string{tech.Session.GetUser().GetId()}
		techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
		updated, err = tech.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, techLine.GetId(), techLine, "status", "assigned_technician_ids")
		require.NoError(t, err)
		assert.Len(t, updated.GetLines()[0].GetAssignedTechnicianIds(), 1)
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, updated.GetLines()[0].GetStatus())

		// Technician CAN close once all lines completed
		closed, err := tech.API.CloseRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())
	})
}

func TestRepairOrderPermissionsViewOnly(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _, orderID, groups := createRepairOrderPermTestEnv(t)

	t.Run("view only (inventory staff)", func(t *testing.T) {
		t.Parallel()
		viewer := fixture.NewVerifiedOwner(t, "rep-viewer")
		cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, viewer.Email)
		require.NoError(t, err)

		// Find Inventory Staff group (view repair orders, no write)
		var viewerGroup *tenantv1.StaffGroup

		for _, g := range groups {
			if g.GetName() == "Inventory Staff" {
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
			_, err = viewer.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, "", &repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS}, "status")
			require.Error(t, err)
			fixture.RequireAPIError(t, err, http.StatusForbidden, "")

			// Viewer CANNOT assign technician
			_, err = viewer.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, "", &repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{""}}, "assigned_technician_ids")
			require.Error(t, err)
			fixture.RequireAPIError(t, err, http.StatusForbidden, "")

			// Viewer CANNOT close
			_, err = viewer.API.CloseRepairOrder(ctx, garageID, orderID)
			require.Error(t, err)
			fixture.RequireAPIError(t, err, http.StatusForbidden, "")
		}
	})
}

func TestRepairOrderPermissionsReadWrite(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _, orderID, groups := createRepairOrderPermTestEnv(t)

	t.Run("read/write (technician)", func(t *testing.T) {
		t.Parallel()
		tech := fixture.NewVerifiedOwner(t, "rep-tech-write")
		cand3, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
		require.NoError(t, err)

		var techGroup *tenantv1.StaffGroup

		for _, g := range groups {
			if g.GetName() == "Technician" {
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
			updated, err := tech.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, techLine.GetId(), techLine, "status")
			require.NoError(t, err)
			assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS, updated.GetLines()[0].GetStatus())

			// Technician can assign self
			techLine.AssignedTechnicianIds = []string{tech.Session.GetUser().GetId()}
			techLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
			updated, err = tech.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, techLine.GetId(), techLine, "status", "assigned_technician_ids")
			require.NoError(t, err)
			assert.Len(t, updated.GetLines()[0].GetAssignedTechnicianIds(), 1)
			assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, updated.GetLines()[0].GetStatus())

			// Technician CAN close once all lines completed
			closed, err := tech.API.CloseRepairOrder(ctx, garageID, orderID)
			require.NoError(t, err)
			assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())
		}
	})
}

func TestRepairOrderPermissionsOwner(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, vehicle, _, _ := createRepairOrderPermTestEnv(t)

	t.Run("owner full access", func(t *testing.T) {
		t.Parallel()
		// Owner can also close a different order
		order2, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
			VehicleId: vehicle.GetId(),
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
				Description: "Owner Close",
				Quantity:    "1",
				UnitPrice:   100000,
			}},
		})
		require.NoError(t, err)

		lines2, err := owner.API.GetRepairOrder(ctx, garageID, order2.GetId())
		require.NoError(t, err)

		ownerLine := lines2.GetLines()[0]
		ownerLine.Status = repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
		_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, order2.GetId(), ownerLine.GetId(), ownerLine, "status")
		require.NoError(t, err)

		closed2, err := owner.API.CloseRepairOrder(ctx, garageID, order2.GetId())
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed2.GetStatus())

		// A closed order cannot be edited further (a line's description is editable since Release 7.3)
		_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garageID, order2.GetId(), ownerLine.GetId(), &repairorderv1.RepairOrderLine{Description: "Try edit"}, "description")
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		// Closing again is refused
		_, err = owner.API.CloseRepairOrder(ctx, garageID, order2.GetId())
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})
}
