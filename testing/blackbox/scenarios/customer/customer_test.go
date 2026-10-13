// Package customer_test covers Customers & Vehicles at API level:
// creation with consent, search, validation, per-garage phone and plate
// uniqueness, edits, deletes, vehicle history, and the vehicle identity
// lock with correction and owner change. Repair history entries,
// badges and pickers are web-E2E.
package customer_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// garageWithOwner returns a fresh owner with one garage.
func garageWithOwner(t *testing.T, label string) (*fixture.Actor, *tenantv1.Garage) {
	t.Helper()

	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	return owner, garage
}

// Scenario: E2E-CUS-001 (partial): a customer is created with consent
// and found by search — the API substance; the detail page and badges
// are web-E2E.
func TestCreateCustomerWithConsentAndSearch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-create")

	phone := "0911000020"
	email := fixture.UniqueEmail(t, "hoa")

	created, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer Hoa Pham", Phone: &phone, Email: &email,
		Address: new("15 Nguyen Hue, Quan 1, TP.HCM"), PersonalDataConsent: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "QA Customer Hoa Pham", created.GetFullName())
	assert.True(t, created.GetPersonalDataConsent())
	assert.EqualValues(t, 0, created.GetVehicleCount())

	got, err := owner.API.GetCustomer(ctx, garage.GetId(), created.GetId())
	require.NoError(t, err)
	assert.Equal(t, created.GetId(), got.GetId())

	for _, query := range []string{"Hoa", "0911000020", "hoa"} {
		found, err := owner.API.SearchCustomers(ctx, garage.GetId(), query)
		require.NoError(t, err, "query %q", query)
		require.NotEmpty(t, found, "query %q", query)
		assert.Equal(t, created.GetId(), found[0].GetId())
	}
}

// Scenario: E2E-CUS-002 (partial): customer field validation — the API
// guards behind the form.
func TestCustomerValidation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-valid")

	// Empty full name is refused.
	_, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{FullName: ""})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Too-short phone is refused.
	_, err = owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Khoa Bui", Phone: new("0911 000")})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Malformed e-mail is refused.
	_, err = owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Khoa Bui", Phone: new("0911000021"), Email: new("khoa@")})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Phone and e-mail are optional.
	saved, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Long Ngo"})
	require.NoError(t, err)
	assert.Empty(t, saved.GetPhone())
}

// Scenario: E2E-CUS-003 (partial): a customer phone is unique within a
// garage only — asserted at API level.
func TestCustomerPhoneUniquePerGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "cus-phone")

	fixture.SwitchPlan(t, owner, "Pro")

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Phone A"})
	require.NoError(t, err)

	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Phone B"})
	require.NoError(t, err)

	phone := "0911000001"

	_, err = owner.API.CreateCustomer(ctx, gA.GetId(),
		&customerv1.Customer{FullName: "QA Customer An", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	_, err = owner.API.CreateCustomer(ctx, gA.GetId(),
		&customerv1.Customer{FullName: "QA Customer An Clone", Phone: &phone})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	clone, err := owner.API.CreateCustomer(ctx, gB.GetId(),
		&customerv1.Customer{FullName: "QA Customer An Clone", Phone: &phone})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteCustomer(ctx, gB.GetId(), clone.GetId()))
}

// Scenario: E2E-CUS-004 (partial): a customer is edited and consent is
// withdrawn and given again — the API substance; the badge is web-E2E.
func TestEditCustomerAndConsent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-edit")

	phone := "0911000041"

	created, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer An Nguyen", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	updated, err := owner.API.UpdateCustomer(ctx, garage.GetId(), created.GetId(),
		&customerv1.Customer{Address: new("20 Pasteur, Quan 3, TP.HCM"), PersonalDataConsent: false},
		"address", "personal_data_consent")
	require.NoError(t, err)
	assert.Equal(t, "20 Pasteur, Quan 3, TP.HCM", updated.GetAddress())
	assert.False(t, updated.GetPersonalDataConsent())

	// The change survives a reload.
	reloaded, err := owner.API.GetCustomer(ctx, garage.GetId(), created.GetId())
	require.NoError(t, err)
	assert.Equal(t, "20 Pasteur, Quan 3, TP.HCM", reloaded.GetAddress())
	assert.False(t, reloaded.GetPersonalDataConsent())

	regiven, err := owner.API.UpdateCustomer(ctx, garage.GetId(), created.GetId(),
		&customerv1.Customer{PersonalDataConsent: true}, "personal_data_consent")
	require.NoError(t, err)
	assert.True(t, regiven.GetPersonalDataConsent())
}

// Scenario: E2E-CUS-005 (partial): a vehicle is registered for a
// customer — the API substance; the customer page count is web-E2E.
func TestRegisterVehicle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-vehicle")

	phone := "0911000051"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Binh Tran", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	year := int32(2019)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51A-55555",
		Make: new("Kia"), Model: new("Morning"), Year: &year,
		Vin: new("KNABE511AKT000001"), EngineNumber: new("G4LA0001"),
	})
	require.NoError(t, err)
	assert.Equal(t, customer.GetId(), vehicle.GetCustomerId())
	assert.Equal(t, "QA Customer Binh Tran", vehicle.GetCustomerName())

	got, err := owner.API.GetVehicle(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	assert.Equal(t, vehicle.GetId(), got.GetId())

	// A vehicle cannot be saved without an owner.
	_, err = owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{LicensePlate: "51A-55556"})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}

// Scenario: E2E-CUS-006 (partial): license plates are unique per garage
// whatever the formatting — asserted at API level.
func TestLicensePlateUniquePerGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "cus-plate")

	fixture.SwitchPlan(t, owner, "Pro")

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Plate A"})
	require.NoError(t, err)

	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Plate B"})
	require.NoError(t, err)

	phoneA := "0911000061"
	phoneB := "0911000062"

	cA, err := owner.API.CreateCustomer(ctx, gA.GetId(),
		&customerv1.Customer{FullName: "QA Customer A", Phone: &phoneA})
	require.NoError(t, err)

	cB, err := owner.API.CreateCustomer(ctx, gB.GetId(),
		&customerv1.Customer{FullName: "QA Customer B", Phone: &phoneB})
	require.NoError(t, err)

	_, err = owner.API.CreateVehicle(ctx, gA.GetId(),
		&customerv1.Vehicle{CustomerId: cA.GetId(), LicensePlate: "51A-11111"})
	require.NoError(t, err)

	// Same plate, different formatting: refused.
	_, err = owner.API.CreateVehicle(ctx, gA.GetId(),
		&customerv1.Vehicle{CustomerId: cA.GetId(), LicensePlate: "51a11111"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	_, err = owner.API.CreateVehicle(ctx, gA.GetId(),
		&customerv1.Vehicle{CustomerId: cA.GetId(), LicensePlate: "51A 111 11"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Other garage: saved.
	other, err := owner.API.CreateVehicle(ctx, gB.GetId(),
		&customerv1.Vehicle{CustomerId: cB.GetId(), LicensePlate: "51A-11111"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteVehicle(ctx, gB.GetId(), other.GetId()))
}

// Scenario: E2E-CUS-007 (partial): vehicle field validation — the API
// guards behind the form.
func TestVehicleValidation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-vvalid")

	phone := "0911000071"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Binh", Phone: &phone})
	require.NoError(t, err)

	newYear := func(y int32) *int32 { return &y }

	// Empty plate is refused.
	_, err = owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "", Year: newYear(2020)})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Too-short plate is refused.
	_, err = owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A", Year: newYear(2020)})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Year out of range is refused.
	_, err = owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-66666", Year: newYear(1885)})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Next year is allowed.
	nextYear := int32(time.Now().Year() + 1) //nolint:gosec // G115: a calendar year always fits int32

	saved, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-66666", Year: &nextYear})
	require.NoError(t, err)

	// Next year + 1 is refused.
	overYear := nextYear + 1
	_, err = owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-66667", Year: &overYear})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	require.NoError(t, owner.API.DeleteVehicle(ctx, garage.GetId(), saved.GetId()))
}

// Scenario: E2E-CUS-008 (partial): vehicles are searched by plate, make
// and model — the API substance; the empty state is web-E2E.
func TestSearchVehicles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-vsearch")

	phone := "0911000081"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Search", Phone: &phone})
	require.NoError(t, err)

	_, err = owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51A-11112", Make: new("Toyota"), Model: new("Vios"),
	})
	require.NoError(t, err)

	for _, query := range []string{"51A-111", "51a111", "Toyota", "Vios"} {
		found, err := owner.API.SearchVehicles(ctx, garage.GetId(), query)
		require.NoError(t, err, "query %q", query)
		require.NotEmpty(t, found, "query %q", query)
	}

	all, err := owner.API.ListVehicles(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, all)

	none, err := owner.API.SearchVehicles(ctx, garage.GetId(), "no-such-vehicle-zzz")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// Scenario: E2E-CUS-009 (partial): a vehicle is deleted, then a customer
// with vehicles — the API substance; the confirm copy is web-E2E.
func TestDeleteVehicleAndCustomer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-delete")

	phone := "0911000030"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Minh Dao", Phone: &phone})
	require.NoError(t, err)

	v1, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-77777"})
	require.NoError(t, err)

	v2, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-88888"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteVehicle(ctx, garage.GetId(), v2.GetId()))

	_, err = owner.API.GetVehicle(ctx, garage.GetId(), v2.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	require.NoError(t, owner.API.DeleteCustomer(ctx, garage.GetId(), customer.GetId()))

	_, err = owner.API.GetVehicle(ctx, garage.GetId(), v1.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	// A deleted vehicle's plate can be reused.
	phone2 := "0911000031"

	customer2, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Reuse", Phone: &phone2})
	require.NoError(t, err)

	reused, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer2.GetId(), LicensePlate: "51A-77777"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteVehicle(ctx, garage.GetId(), reused.GetId()))
}

// Scenario: E2E-CUS-005 (partial): a vehicle that quotes refer to is
// deactivated instead of deleted, then reactivated — the API lifecycle
// behind the pickers.
func TestActivateVehicle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-activate")

	phone := "0911000091"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Active", Phone: &phone})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-99991"})
	require.NoError(t, err)

	// A draft quote refers to the vehicle: deleting deactivates instead.
	quote, err := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, quote.GetId())

	require.NoError(t, owner.API.DeleteVehicle(ctx, garage.GetId(), vehicle.GetId()))

	deactivated, err := owner.API.GetVehicle(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	assert.False(t, deactivated.GetIsActive())

	reactivated, err := owner.API.ActivateVehicle(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	assert.True(t, reactivated.GetIsActive())
}

// Scenario: E2E-CUS-009 (partial): \1 —: a customer
// deactivated instead of deleted (a live quote refers to them) is
// reactivated — the API lifecycle behind the pickers.
func TestActivateCustomer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-cactivate")

	phone := "0911000111"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Deactivate", Phone: &phone})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(),
		&customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-44441"})
	require.NoError(t, err)

	quote, err := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, quote.GetId())

	// A live quote refers to the customer: deleting deactivates instead.
	require.NoError(t, owner.API.DeleteCustomer(ctx, garage.GetId(), customer.GetId()))

	deactivated, err := owner.API.GetCustomer(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)
	assert.False(t, deactivated.GetIsActive())

	reactivated, err := owner.API.ActivateCustomer(ctx, garage.GetId(), customer.GetId())
	require.NoError(t, err)
	assert.True(t, reactivated.GetIsActive())
}

// Scenario: E2E-CUS-012 (partial): cross-garage isolation for customers
// and vehicles — the API substance of the permission matrix; the sidebar
// and role screens are web-E2E.
func TestCustomerCrossGarageIsolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "cus-isolation")

	other := fixture.NewVerifiedOwner(t, "cus-stranger")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Cus Iso"})
	require.NoError(t, err)

	phone := "0911000101"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Iso", Phone: &phone})
	require.NoError(t, err)

	_, err = other.API.GetCustomer(ctx, garage.GetId(), customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	_, err = other.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")
}
