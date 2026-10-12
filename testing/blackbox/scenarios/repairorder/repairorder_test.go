// Package repairorder_test covers Repair Orders at API level: ad-hoc
// creation and validation, line progress with derived board status,
// technician assignment, closing guards, closed-order immutability,
// vehicle history, and listing. The Kanban drag, photos and realtime
// tracking are web-E2E or later phases.
package repairorder_test

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// setup returns a fresh owner, garage and vehicle for repair-order
// scenarios.
func setup(t *testing.T, label string) (*fixture.Actor, *tenantv1.Garage, *customerv1.Vehicle) {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09120" + suffix

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51R-" + suffix,
	})
	require.NoError(t, err)

	return owner, garage, vehicle
}

func laborLine(description, quantity string, unitPrice int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
		Description: description, Quantity: quantity, UnitPrice: unitPrice,
	}
}

func partLine(description, quantity string, unitPrice int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
		Description: description, Quantity: quantity, UnitPrice: unitPrice,
	}
}

// adhocOrder creates an ad-hoc repair order with a labor and a part
// line (total 600,000 VND).
func adhocOrder(t *testing.T, api *client.Client, garageID, vehicleID string) *repairorderv1.RepairOrder {
	t.Helper()

	order, err := api.CreateRepairOrder(t.Context(), garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicleID,
		Lines: []*repairorderv1.RepairOrderLine{
			laborLine("Brake inspection", "1", 150000),
			partLine("Brake pad set", "1", 450000),
		},
	})
	require.NoError(t, err)

	return order
}

func completeLine(t *testing.T, api *client.Client, garageID, orderID, lineID string) *repairorderv1.RepairOrder {
	t.Helper()

	updated, err := api.UpdateRepairOrderLine(t.Context(), garageID, orderID, lineID,
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	return updated
}

// Scenario: E2E-REP-001 (partial): an ad-hoc repair order is created —
// the API substance (number, derived To Do status, total); the board
// column is web-E2E.
func TestCreateAdhocRepairOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-create")

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())
	assert.NotEmpty(t, order.GetNumber())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, order.GetStatus())
	assert.EqualValues(t, 600000, order.GetTotalAmount())
	assert.Equal(t, vehicle.GetLicensePlate(), order.GetVehicleLicensePlate())

	got, err := owner.API.GetRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)
	assert.Equal(t, order.GetId(), got.GetId())

	listed, err := owner.API.ListRepairOrders(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, listed)
}

// Scenario: E2E-REP-002 (partial): ad-hoc repair order validation — the
// API guards behind the form.
func TestAdhocRepairOrderValidation(t *testing.T) {
	t.Parallel()

	owner, garage, vehicle := setup(t, "rep-valid")

	// No lines is refused.
	_, err := owner.API.CreateRepairOrder(t.Context(), garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Empty description is refused.
	_, err = owner.API.CreateRepairOrder(t.Context(), garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines:     []*repairorderv1.RepairOrderLine{laborLine("", "1", 150000)},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Zero quantity is refused.
	_, err = owner.API.CreateRepairOrder(t.Context(), garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines:     []*repairorderv1.RepairOrderLine{laborLine("Brake inspection", "0", 150000)},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}

// Scenario: E2E-REP-004 (partial) + E2E-REP-006 (partial): line progress
// drives the derived order status — asserted at API level; the board
// columns are web-E2E.
func TestLineProgressDerivesStatus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-progress")

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, order.GetStatus())

	// One line in progress: In Progress.
	progress, err := owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, progress.GetStatus())

	// First completed, second in progress: still In Progress.
	progress = completeLine(t, owner.API, garage.GetId(), order.GetId(), order.GetLines()[0].GetId())

	progress, err = owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), progress.GetLines()[1].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, progress.GetStatus())

	// Both completed: Completed.
	done := completeLine(t, owner.API, garage.GetId(), order.GetId(), progress.GetLines()[1].GetId())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, done.GetStatus())
}

// Scenario: E2E-REP-005 (partial): technicians are assigned to a line —
// the API substance; the checkbox list is web-E2E.
func TestAssignTechnicians(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-assign")

	groups, err := owner.API.ListStaffGroups(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, groups)

	tech := fixture.NewVerifiedOwner(t, "rep-tech")

	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), tech.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	mine, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, mine)

	_, err = tech.API.AcceptInvitation(ctx, mine[0].GetId())
	require.NoError(t, err)

	techID := tech.Session.GetUser().GetId()

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())

	assigned, err := owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{techID}}, "assigned_technician_ids")
	require.NoError(t, err)
	assert.Contains(t, assigned.GetLines()[0].GetAssignedTechnicianIds(), techID)

	// A status-only save keeps the technicians.
	kept, err := owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS}, "status")
	require.NoError(t, err)
	assert.Contains(t, kept.GetLines()[0].GetAssignedTechnicianIds(), techID)

	// Unassigning leaves zero technicians.
	cleared, err := owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{}}, "assigned_technician_ids")
	require.NoError(t, err)
	assert.Empty(t, cleared.GetLines()[0].GetAssignedTechnicianIds())

	// The technician filter lists the order while assigned.
	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{techID}}, "assigned_technician_ids")
	require.NoError(t, err)

	filtered, err := owner.API.ListRepairOrdersByTechnician(ctx, garage.GetId(), techID)
	require.NoError(t, err)
	require.NotEmpty(t, filtered)

	unfiltered, err := owner.API.ListRepairOrders(ctx, garage.GetId())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(unfiltered), len(filtered))
}

// Scenario: E2E-REP-008 (partial): closing requires every line
// completed — the API guards; the button hint is web-E2E.
func TestCloseRequiresCompletedLines(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-close")

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())

	// Closing with open lines is refused.
	_, err := owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	done := completeLine(t, owner.API, garage.GetId(), order.GetId(), order.GetLines()[0].GetId())
	done = completeLine(t, owner.API, garage.GetId(), order.GetId(), done.GetLines()[1].GetId())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, done.GetStatus())

	closed, err := owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())
	assert.NotNil(t, closed.GetClosedAt())
}

// Scenario: E2E-REP-009 (partial): a closed repair order cannot be
// edited further — asserted at API level.
func TestClosedOrderImmutable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-closed")

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())

	completeLine(t, owner.API, garage.GetId(), order.GetId(), order.GetLines()[0].GetId())

	completed := completeLine(t, owner.API, garage.GetId(), order.GetId(), order.GetLines()[1].GetId())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, completed.GetStatus())

	_, err := owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)

	// Editing a line of a closed order is refused.
	_, err = owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Closing again is refused.
	_, err = owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "")
}

// Scenario: E2E-REP-010 (partial): the repair order joins the vehicle's
// repair history — asserted at API level; following the link is
// web-E2E.
func TestRepairOrderInVehicleHistory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-history")

	order := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())

	history, err := owner.API.GetVehicleHistory(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, history)
	assert.Equal(t, order.GetNumber(), history[0].GetNumber())
}
