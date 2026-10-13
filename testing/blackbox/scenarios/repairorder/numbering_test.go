package repairorder_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-REP-011 (partial): a repair order number is rendered from
// the garage's template with consecutive sequences, and each garage
// counts independently of the other garages of the same owner.
func TestRepairOrderNumberFromTemplate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage1, vehicle1 := setup(t, "rep-number")

	template := &tenantv1.NumberingTemplates{RepairOrder: "LSC-{YYYY}{MM}-{SEQ:4}"}

	_, err := owner.API.UpdateGarage(ctx, garage1.GetId(), &tenantv1.Garage{Numbering: template}, "numbering.repair_order")
	require.NoError(t, err)

	first := adhocOrder(t, owner.API, garage1.GetId(), vehicle1.GetId())
	second := adhocOrder(t, owner.API, garage1.GetId(), vehicle1.GetId())

	prefix := "LSC-" + time.Now().In(time.FixedZone("ICT", 7*3600)).Format("200601") + "-"
	assert.Equal(t, prefix+"0001", first.GetNumber())
	assert.Equal(t, prefix+"0002", second.GetNumber())

	// Another garage of the same owner starts its own sequence.
	fixture.SwitchPlan(t, owner, "pro") // two garages
	garage2, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Numbers 2"})
	require.NoError(t, err)

	_, err = owner.API.UpdateGarage(ctx, garage2.GetId(), &tenantv1.Garage{Numbering: template}, "numbering.repair_order")
	require.NoError(t, err)

	vehicle2 := newVehicle(t, owner, garage2.GetId(), "rep-number-2")
	other := adhocOrder(t, owner.API, garage2.GetId(), vehicle2.GetId())
	assert.Equal(t, prefix+"0001", other.GetNumber())
}

// Scenario: E2E-REP-007 (partial): filtering the board by technician
// lists only orders with a line assigned to them, and clearing the filter
// lists every order. The dropdown and columns are web-E2E.
func TestFilterByTechnician(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, vehicle := setup(t, "rep-filter")

	groups, err := owner.API.ListStaffGroups(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, groups)

	tech := fixture.NewVerifiedOwner(t, "rep-filter-tech")
	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), tech.Email)
	require.NoError(t, err)

	inv, err := owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	_, err = tech.API.AcceptInvitation(ctx, inv.GetId())
	require.NoError(t, err)

	techID := tech.Session.GetUser().GetId()

	assigned := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())
	unassigned := adhocOrder(t, owner.API, garage.GetId(), vehicle.GetId())

	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), assigned.GetId(), assigned.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{techID}}, "assigned_technician_ids")
	require.NoError(t, err)

	filtered, err := owner.API.ListRepairOrdersByTechnician(ctx, garage.GetId(), techID)
	require.NoError(t, err)

	ids := make([]string, 0, len(filtered))
	for _, o := range filtered {
		ids = append(ids, o.GetId())
	}

	assert.Equal(t, []string{assigned.GetId()}, ids, "only the order with the technician")

	all, err := owner.API.ListRepairOrders(ctx, garage.GetId())
	require.NoError(t, err)

	allIDs := make([]string, 0, len(all))
	for _, o := range all {
		allIDs = append(allIDs, o.GetId())
	}

	assert.ElementsMatch(t, []string{assigned.GetId(), unassigned.GetId()}, allIDs, "cleared: every order")
}

// newVehicle registers a customer and vehicle in another garage of owner.
func newVehicle(t *testing.T, owner *fixture.Actor, garageID, label string) *customerv1.Vehicle {
	t.Helper()

	ctx := t.Context()
	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09121" + suffix

	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51T-" + suffix,
	})
	require.NoError(t, err)

	return vehicle
}
