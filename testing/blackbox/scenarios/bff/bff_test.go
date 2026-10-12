// Package bff_test covers the mobile BFFs at API level: the
// technician's work list and the customer portal (garage links,
// vehicles and vehicle history). Push, devices and the app screens
// are out of API scope.
package bff_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// inviteTechnician registers a fresh verified user, invites them to the
// garage and accepts; it returns the technician actor.
func inviteTechnician(t *testing.T, owner *fixture.Actor, garageID, label string) *fixture.Actor {
	t.Helper()

	ctx := t.Context()

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)

	var technicianID string

	for _, g := range groups {
		if g.GetName() == "Technician" {
			technicianID = g.GetId()
		}
	}

	require.NotEmpty(t, technicianID, "no Technician group in garage %s", garageID)

	tech := fixture.NewVerifiedOwner(t, label)

	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{technicianID})
	require.NoError(t, err)

	mine, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, mine)

	_, err = tech.API.AcceptInvitation(ctx, mine[0].GetId())
	require.NoError(t, err)

	return tech
}

// Scenario: E2E-REP-005 (partial): the technician's
// work list shows open orders with their assigned lines — asserted at
// API level; the app screen is mobile-E2E.
func TestTechnicianWorkList(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "bff-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox BFF Work"})
	require.NoError(t, err)

	tech := inviteTechnician(t, owner, garage.GetId(), "bff-tech")
	techID := tech.Session.GetUser().GetId()

	phone := "0914000001"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer BFF", Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51T-12345",
	})
	require.NoError(t, err)

	order, err := owner.API.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "Brake inspection", Quantity: "1", UnitPrice: 150000,
		}},
	})
	require.NoError(t, err)

	_, err = owner.API.UpdateRepairOrderLineMasked(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: []string{techID}}, "assigned_technician_ids")
	require.NoError(t, err)

	work, err := tech.API.ListMyWork(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, work)
	assert.Equal(t, order.GetId(), work[0].GetRepairOrderId())
	assert.Equal(t, garage.GetId(), work[0].GetGarageId())
	require.NotEmpty(t, work[0].GetLines())

	// An unrelated user sees no work.
	stranger := fixture.NewVerifiedOwner(t, "bff-stranger")

	empty, err := stranger.API.ListMyWork(ctx)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Anonymous callers are refused.
	_, err = fixture.Anonymous(t).ListMyWork(ctx)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
}

// Scenario: E2E-CUS-004 (partial): a customer profile is
// linked to its user, who then sees their vehicles and history through
// the customer portal — asserted at API level; the app screens are
// mobile-E2E.
func TestCustomerPortalLinkAndVehicles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bff-garage")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox BFF Portal"})
	require.NoError(t, err)

	person := fixture.NewVerifiedOwner(t, "bff-person")

	phone := "0914000002"
	email := person.Email

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer Portal", Phone: &phone, Email: &email, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51P-54321",
	})
	require.NoError(t, err)

	link, err := owner.API.RequestCustomerUserLink(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)
	assert.NotEmpty(t, link.GetId())

	links, err := person.API.ListMyGarageLinks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, links)

	accepted, err := person.API.AcceptGarageLink(ctx, links[0].GetId())
	require.NoError(t, err)
	assert.Equal(t, links[0].GetId(), accepted.GetId())

	vehicles, err := person.API.ListMyVehicles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, vehicles)
	assert.Equal(t, garage.GetId(), vehicles[0].GetGarageId())
	require.NotEmpty(t, vehicles[0].GetVehicles())
	assert.Equal(t, vehicle.GetId(), vehicles[0].GetVehicles()[0].GetId())

	history, err := person.API.GetMyVehicleHistory(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	assert.NotNil(t, history)

	// Ending the link from the user's side hides the vehicles again.
	unlinked, err := person.API.UnlinkGarageLink(ctx, links[0].GetId())
	require.NoError(t, err)
	assert.Equal(t, links[0].GetId(), unlinked.GetId())

	after, err := person.API.ListMyVehicles(ctx)
	require.NoError(t, err)
	assert.Empty(t, after)
}

// Scenario: E2E-CUS-004 (partial): a garage's link
// request can be declined from the user's side — asserted at API
// level; the app screens are mobile-E2E.
func TestCustomerPortalDeclineLink(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bff-decline")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox BFF Decline"})
	require.NoError(t, err)

	person := fixture.NewVerifiedOwner(t, "bff-decliner")

	phone := "0914000003"
	email := person.Email

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer Decline", Phone: &phone, Email: &email, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	_, err = owner.API.RequestCustomerUserLink(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)

	links, err := person.API.ListMyGarageLinks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, links)

	declined, err := person.API.DeclineGarageLink(ctx, links[0].GetId())
	require.NoError(t, err)
	assert.Equal(t, links[0].GetId(), declined.GetId())

	vehicles, err := person.API.ListMyVehicles(ctx)
	require.NoError(t, err)
	assert.Empty(t, vehicles)
}

// Scenario: E2E-CUS-004 (partial): the garage ends a link
// from its side while the profile stays — asserted at API level.
func TestCustomerPortalGarageSideUnlink(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bff-gunlink")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox BFF GUnlink"})
	require.NoError(t, err)

	person := fixture.NewVerifiedOwner(t, "bff-gperson")

	phone := "0914000004"
	email := person.Email

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer GUnlink", Phone: &phone, Email: &email, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	_, err = owner.API.RequestCustomerUserLink(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)

	links, err := person.API.ListMyGarageLinks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, links)

	_, err = person.API.AcceptGarageLink(ctx, links[0].GetId())
	require.NoError(t, err)

	unlinked, err := owner.API.UnlinkCustomerUser(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)
	assert.NotNil(t, unlinked)

	// The profile stays; the person sees no vehicles.
	profile, err := owner.API.GetCustomer(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)
	assert.Equal(t, customer.GetId(), profile.GetId())

	vehicles, err := person.API.ListMyVehicles(ctx)
	require.NoError(t, err)
	assert.Empty(t, vehicles)
}

// waitForRoles polls until the owner's default roles arrive (created by
// the async outbox consumer after verification).
func waitForRoles(t *testing.T, owner *fixture.Actor) []*tenantv1.Role {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		roles, err := owner.API.ListRoles(t.Context())
		require.NoError(t, err)

		if len(roles) > 0 {
			return roles
		}

		if time.Now().After(deadline) {
			t.Fatal("default roles never arrived")
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// Scenario: E2E-CUS-012 (partial): the permission catalogue
// and role reads back the technician BFF guards — asserted at API
// level; the sidebar is web-E2E.
func TestPermissionCatalogueAndRoleRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bff-perms")

	permissions, err := owner.API.ListPermissions(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, permissions)

	// Default roles arrive through the async outbox relay: wait for them.
	roles := waitForRoles(t, owner)
	require.NotEmpty(t, roles)

	role, err := owner.API.GetRole(ctx, roles[0].GetId())
	require.NoError(t, err)
	assert.Equal(t, roles[0].GetId(), role.GetId())

	_, err = owner.API.GetMyInvitation(ctx, "00000000-0000-4000-8000-000000000000")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}
