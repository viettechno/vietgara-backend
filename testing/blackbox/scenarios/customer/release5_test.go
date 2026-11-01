// Releases 5.0 and 5.1: walk-in customers, link requests by code, tax
// profiles, the vehicle edit lock with its corrections, owner change, and
// delete-or-deactivate. Documented as E2E-CUS-013 to E2E-CUS-022.
package customer_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func laborQuote(customerID, vehicleID string) *quotev1.Quote {
	return &quotev1.Quote{
		CustomerId: customerID, VehicleId: vehicleID,
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	}
}

// newestOTPAfter waits for an e-mail to address beyond the before that were
// there already, and returns the code in it.
func newestOTPAfter(t *testing.T, address string, before int) string {
	t.Helper()

	mail := fixture.Mailpit(t)

	var code string

	require.Eventually(t, func() bool {
		messages, err := mail.Messages(t.Context(), address)
		if err != nil || len(messages) <= before {
			return false
		}

		otp, otpErr := messages[0].OTP()
		code = otp

		return otpErr == nil
	}, 20*time.Second, 300*time.Millisecond, "a code arrives for %s", address)

	return code
}

func mailCount(t *testing.T, address string) int {
	t.Helper()

	messages, err := fixture.Mailpit(t).Messages(t.Context(), address)
	require.NoError(t, err)

	return len(messages)
}

// Scenario: E2E-CUS-013: a walk-in customer has no user behind it; a quote
// is made for it and there is nobody to ask for a link.
func TestWalkInCustomer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-walkin")
	phone := "0911000101"

	customer, err := business.Owner.API.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Walk In", Phone: &phone})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNSPECIFIED, customer.GetLinkStatus(), "never linked")
	assert.False(t, customer.GetPersonalDataConsent(), "no consent yet")

	vehicle, err := business.Owner.API.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51A-10101"})
	require.NoError(t, err)

	quote, err := business.Owner.API.CreateQuote(ctx, business.GarageID, laborQuote(customer.GetId(), vehicle.GetId()))
	require.NoError(t, err, "a walk-in customer can be quoted")
	assert.Equal(t, customer.GetId(), quote.GetCustomerId())

	_, err = business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
	require.Error(t, err, "without an e-mail there is nobody to ask")
	assert.Equal(t, http.StatusConflict, client.AsAPIError(err).Status, "%v", err)
}

// Scenario: E2E-CUS-014: the customer search matches a phone or an
// e-mail inside the garage and never reaches another garage's customers.
func TestSearchCustomersByPhoneOrEmail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mine := fixture.NewBusiness(t, "cus-search")
	other := fixture.NewBusiness(t, "cus-search-other")

	phone, email := "0911000121", "search.me@vietgara.test"

	customer, err := mine.Owner.API.CreateCustomer(ctx, mine.GarageID, &customerv1.Customer{FullName: "QA Customer Searchable", Phone: &phone, Email: &email})
	require.NoError(t, err)

	for _, query := range []string{phone, email, "Searchable", "search.me"} {
		found, searchErr := mine.Owner.API.SearchCustomers(ctx, mine.GarageID, query)
		require.NoError(t, searchErr)
		require.Len(t, found, 1, "query %q", query)
		assert.Equal(t, customer.GetId(), found[0].GetId())
	}

	for _, query := range []string{phone, email} {
		found, searchErr := other.Owner.API.SearchCustomers(ctx, other.GarageID, query)
		require.NoError(t, searchErr)
		assert.Empty(t, found, "another garage never finds it: %q", query)
	}
}

// Scenario: E2E-CUS-015: a link request is confirmed with the code the
// user reads out; the user then lists the garage.
func TestLinkRequestConfirmedWithTheCode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-link")
	person := fixture.NewVerifiedOwner(t, "cus-link-person")

	email := person.Email

	customer, err := business.Owner.API.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Link", Email: &email, PersonalDataConsent: true})
	require.NoError(t, err)

	before := mailCount(t, person.Email)

	link, err := business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_REQUESTED, link.GetStatus())

	code := newestOTPAfter(t, person.Email, before)

	confirmed, err := business.Owner.API.ConfirmCustomerUserLink(ctx, business.GarageID, customer.GetId(), code)
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED, confirmed.GetStatus())
	assert.Equal(t, customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_OTP, confirmed.GetChannel())

	got, err := business.Owner.API.GetCustomer(ctx, business.GarageID, customer.GetId())
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED, got.GetLinkStatus())

	links, err := person.API.ListMyGarageLinks(ctx)
	require.NoError(t, err)

	garages := make([]string, 0, len(links))
	for _, l := range links {
		garages = append(garages, l.GetGarageId())
	}

	assert.Contains(t, garages, business.GarageID, "the user sees the garage that holds their profile")

	_, err = business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_EXISTS")
}

// Scenario: E2E-CUS-016: nobody to ask, a wrong code, a decline, ending the
// link from the garage, asking again, and who may ask.
func TestLinkRequestEdgeCases(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-link-edge")
	person := fixture.NewVerifiedOwner(t, "cus-link-edge-person")

	t.Run("an e-mail that no verified user owns has nobody to ask", func(t *testing.T) {
		nobody := "nobody.owns.this@vietgara.test"

		customer, err := business.Owner.API.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Nobody", Email: &nobody})
		require.NoError(t, err)

		_, err = business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
		require.Error(t, err)
		assert.Equal(t, http.StatusConflict, client.AsAPIError(err).Status, "%v", err)
	})

	email := person.Email

	customer, err := business.Owner.API.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Edge", Email: &email})
	require.NoError(t, err)

	before := mailCount(t, person.Email)

	_, err = business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
	require.NoError(t, err)

	code := newestOTPAfter(t, person.Email, before)

	t.Run("asking again at once waits for the cooldown", func(t *testing.T) {
		_, err := business.Owner.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
		fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "OTP_RESEND_TOO_SOON")
	})

	t.Run("a wrong code is refused and the right one still works", func(t *testing.T) {
		wrong := "000000"
		if code == wrong {
			wrong = "111111"
		}

		_, err := business.Owner.API.ConfirmCustomerUserLink(ctx, business.GarageID, customer.GetId(), wrong)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_INVALID")

		_, err = business.Owner.API.ConfirmCustomerUserLink(ctx, business.GarageID, customer.GetId(), code)
		require.NoError(t, err)
	})

	t.Run("the garage ends the link; the profile stays", func(t *testing.T) {
		unlinked, err := business.Owner.API.UnlinkCustomerUser(ctx, business.GarageID, customer.GetId())
		require.NoError(t, err)
		assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNLINKED, unlinked.GetStatus())

		got, err := business.Owner.API.GetCustomer(ctx, business.GarageID, customer.GetId())
		require.NoError(t, err)
		assert.Equal(t, "QA Customer Edge", got.GetFullName())

		_, err = business.Owner.API.UnlinkCustomerUser(ctx, business.GarageID, customer.GetId())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})

	t.Run("a member who may not write customers cannot ask", func(t *testing.T) {
		technician := business.AddStaff(t, "Technician")

		_, err := technician.API.RequestCustomerUserLink(ctx, business.GarageID, customer.GetId())
		require.Error(t, err)
		assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, client.AsAPIError(err).Status)
	})
}

// Scenario: E2E-CUS-017: one personal profile, any number of company
// profiles, the tax ID check, and a default that moves.
func TestTaxProfiles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-tax")
	api := business.Owner.API

	personal := &customerv1.CustomerTaxProfile{Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL, LegalName: "Pham Thi Hoa", TaxId: new("0123456789")}

	first, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, personal)
	require.NoError(t, err)
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL, first.GetKind())

	_, err = api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, personal)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	company, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "QA Company Hoa Binh JSC", TaxId: new("0312345678"),
		Address: new("1 Nguyen Hue, Quan 1"), InvoiceEmail: new("ketoan@hoabinh.vietgara.test"), IsDefault: true,
	})
	require.NoError(t, err)
	assert.True(t, company.GetIsDefault())

	branch, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "QA Company Hoa Binh Branch", TaxId: new("0312345678 001"),
	})
	require.NoError(t, err, "a second company profile with the branch suffix")
	assert.Equal(t, "0312345678001", branch.GetTaxId(), "separators are dropped")
	assert.False(t, branch.GetIsDefault())

	t.Run("the tax ID is 10 digits, or 13 with the branch suffix", func(t *testing.T) {
		for _, bad := range []string{"12345", "123456789012", "12345678901234", "abcdefghij"} {
			_, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
				Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "QA Company Bad", TaxId: new(bad),
			})
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
		}

		_, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
			Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, TaxId: new("0312345678"),
		})
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "") // a legal name is required
	})

	t.Run("clearing an optional field", func(t *testing.T) {
		updated, err := api.UpdateTaxProfile(ctx, business.GarageID, business.CustomerID, company.GetId(),
			&customerv1.CustomerTaxProfile{InvoiceEmail: new("")}, "invoice_email")
		require.NoError(t, err)
		assert.Empty(t, updated.GetInvoiceEmail())
		assert.Equal(t, "QA Company Hoa Binh JSC", updated.GetLegalName())
	})

	t.Run("the default moves to the profile that asks for it", func(t *testing.T) {
		_, err := api.UpdateTaxProfile(ctx, business.GarageID, business.CustomerID, branch.GetId(),
			&customerv1.CustomerTaxProfile{IsDefault: true}, "is_default")
		require.NoError(t, err)

		profiles, err := api.ListTaxProfiles(ctx, business.GarageID, business.CustomerID, false)
		require.NoError(t, err)
		require.Len(t, profiles, 3)
		assert.Equal(t, branch.GetId(), profiles[0].GetId(), "the default comes first")

		defaults := 0

		for _, profile := range profiles {
			if profile.GetIsDefault() {
				defaults++
			}
		}

		assert.Equal(t, 1, defaults)
	})

	t.Run("staff without customer permissions are turned away; an advisor can read", func(t *testing.T) {
		technician := business.AddStaff(t, "Technician")
		// A technician has no customer permissions at all.
		_, err := technician.API.ListTaxProfiles(ctx, business.GarageID, business.CustomerID, false)
		require.Error(t, err)

		_, err = technician.API.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, personal)
		require.Error(t, err)

		advisor := business.AddStaff(t, "Service Advisor")
		_, err = advisor.API.ListTaxProfiles(ctx, business.GarageID, business.CustomerID, false)
		require.NoError(t, err)
	})
}

// Scenario: E2E-CUS-018: a profile a document is billed to is deactivated,
// not deleted, and can be reactivated.
func TestTaxProfileLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-tax-life")
	api := business.Owner.API

	used, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "QA Company Used", TaxId: new("0312345678"), IsDefault: true,
	})
	require.NoError(t, err)

	unused, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "QA Company Unused", TaxId: new("0312345679"),
	})
	require.NoError(t, err)

	// A quote for the customer takes the default profile and keeps it.
	quote := laborQuote(business.CustomerID, business.VehicleID)
	quote.BillingParty = &commonv1.BillingParty{TaxProfileId: used.GetId()}

	created, err := api.CreateQuote(ctx, business.GarageID, quote)
	require.NoError(t, err)
	assert.Equal(t, "QA Company Used", created.GetBillingParty().GetLegalName())

	outcome, err := api.DeleteTaxProfile(ctx, business.GarageID, business.CustomerID, unused.GetId(), true)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, outcome, "the preview says deleted")

	outcome, err = api.DeleteTaxProfile(ctx, business.GarageID, business.CustomerID, unused.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, outcome)

	outcome, err = api.DeleteTaxProfile(ctx, business.GarageID, business.CustomerID, used.GetId(), true)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, outcome, "the preview says deactivated")

	outcome, err = api.DeleteTaxProfile(ctx, business.GarageID, business.CustomerID, used.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, outcome)

	active, err := api.ListTaxProfiles(ctx, business.GarageID, business.CustomerID, false)
	require.NoError(t, err)
	assert.Empty(t, active, "neither is offered any more")

	all, err := api.ListTaxProfiles(ctx, business.GarageID, business.CustomerID, true)
	require.NoError(t, err)
	require.Len(t, all, 1, "the used one is still on file")
	assert.False(t, all[0].GetIsActive())
	assert.False(t, all[0].GetIsDefault(), "it loses its default mark")

	again := laborQuote(business.CustomerID, business.VehicleID)
	again.BillingParty = &commonv1.BillingParty{TaxProfileId: used.GetId()}
	_, err = api.CreateQuote(ctx, business.GarageID, again)
	require.Error(t, err, "an inactive profile cannot be chosen")

	old, err := api.GetQuote(ctx, business.GarageID, created.GetId())
	require.NoError(t, err)
	assert.Equal(t, "QA Company Used", old.GetBillingParty().GetLegalName(), "the existing quote keeps it")

	back, err := api.ActivateTaxProfile(ctx, business.GarageID, business.CustomerID, used.GetId())
	require.NoError(t, err)
	assert.True(t, back.GetIsActive())
	assert.False(t, back.GetIsDefault(), "reactivating does not make it the default again")
}

// Scenario: E2E-CUS-019: a live document locks all six identity fields;
// writing the same value is fine; deleting the only quote unlocks them.
func TestVehicleIdentityLockInDetail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-lock-detail")
	api := business.Owner.API

	vehicle, err := api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{
		CustomerId: business.CustomerID, LicensePlate: "51A-19019", Make: new("Toyota"), Model: new("Vios"), Year: new(int32(2019)),
		Vin: new("VIN19019"), EngineNumber: new("ENG19019"),
	})
	require.NoError(t, err)
	assert.True(t, vehicle.GetEditable())
	assert.Empty(t, vehicle.GetLockedFields())

	// Nothing refers to it yet: a plate change is saved, and back.
	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{LicensePlate: "51A-19020"}, "license_plate")
	require.NoError(t, err)
	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{LicensePlate: "51A-19019"}, "license_plate")
	require.NoError(t, err)

	quote, err := api.CreateQuote(ctx, business.GarageID, laborQuote(business.CustomerID, vehicle.GetId()))
	require.NoError(t, err)

	locked, err := api.GetVehicle(ctx, business.GarageID, vehicle.GetId())
	require.NoError(t, err)
	assert.False(t, locked.GetEditable())
	assert.Len(t, locked.GetLockedFields(), 6, "all six identity fields: %v", locked.GetLockedFields())

	edits := map[string]*customerv1.Vehicle{
		"license_plate": {LicensePlate: "51A-19099"},
		"make":          {Make: new("Honda")},
		"model":         {Model: new("City")},
		"year":          {Year: new(int32(2020))},
		"vin":           {Vin: new("OTHERVIN")},
		"engine_number": {EngineNumber: new("OTHERENG")},
	}

	for mask, edit := range edits {
		_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), edit, mask)
		fixture.RequireAPIError(t, err, http.StatusConflict, "VEHICLE_FIELD_LOCKED")
	}

	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{LicensePlate: "51A-19019"}, "license_plate")
	require.NoError(t, err, "writing the value it already has is not a change")

	otherCustomer, err := api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Other Owner"})
	require.NoError(t, err)

	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{CustomerId: otherCustomer.GetId()}, "customer_id")
	require.Error(t, err, "the owner never changes through an update")

	require.NoError(t, api.DeleteQuote(ctx, business.GarageID, quote.GetId()))

	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{LicensePlate: "51A-19021"}, "license_plate")
	require.NoError(t, err, "a deleted quote does not count")

	order := business.OpenOrderFor(t, vehicle.GetId())
	assert.NotEmpty(t, order.GetId())

	_, err = api.UpdateVehicle(ctx, business.GarageID, vehicle.GetId(), &customerv1.Vehicle{LicensePlate: "51A-19022"}, "license_plate")
	fixture.RequireAPIError(t, err, http.StatusConflict, "VEHICLE_FIELD_LOCKED")
}

// Scenario: E2E-CUS-020: only staff with customer.correct correct a locked
// field, with a reason, and a plate already in use is refused.
func TestCorrectionNeedsThePermissionAndAReason(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-correct")
	api := business.Owner.API

	vehicle, err := api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: business.CustomerID, LicensePlate: "51A-19090", Vin: new("VIN19090")})
	require.NoError(t, err)

	_, err = api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: business.CustomerID, LicensePlate: "51A-19091"})
	require.NoError(t, err)

	_, err = api.CreateQuote(ctx, business.GarageID, laborQuote(business.CustomerID, vehicle.GetId()))
	require.NoError(t, err)

	advisor := business.AddStaff(t, "Service Advisor")
	manager := business.AddStaff(t, "Manager")

	plate := customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_LICENSE_PLATE

	_, err = advisor.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(), plate, "51A-19092", "Typo")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	_, err = manager.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(), plate, "51A-19092", "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = manager.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(), plate, "51A-19091", "Typo")
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_EXISTS")

	corrected, err := manager.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(), plate, "51A-19092", "Typed wrong at the counter")
	require.NoError(t, err)
	assert.Equal(t, "51A-19092", corrected.GetLicensePlate())

	_, err = manager.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(),
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_YEAR, "abc", "Typo")
	require.Error(t, err, "a year is digits")

	cleared, err := manager.API.CorrectVehicle(ctx, business.GarageID, vehicle.GetId(),
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_VIN, "", "Wrong VIN, unknown")
	require.NoError(t, err)
	assert.Empty(t, cleared.GetVin(), "a blank value clears an optional field")
}

// Scenario: E2E-CUS-021: the owner of a vehicle changes; a reason is
// optional; the same owner, another garage's customer and a member without
// the permission are refused; the vehicle's history stays with it.
func TestChangeVehicleOwner(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-owner")
	other := fixture.NewBusiness(t, "cus-owner-other")
	api := business.Owner.API

	next, err := api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer New Owner"})
	require.NoError(t, err)

	closed := business.ClosedOrder(t)
	require.NotEmpty(t, closed.GetId())

	moved, err := api.ChangeVehicleOwner(ctx, business.GarageID, business.VehicleID, next.GetId(), "")
	require.NoError(t, err, "the reason is optional")
	assert.Equal(t, next.GetId(), moved.GetCustomerId())

	_, err = api.ChangeVehicleOwner(ctx, business.GarageID, business.VehicleID, next.GetId(), "again")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = api.ChangeVehicleOwner(ctx, business.GarageID, business.VehicleID, other.CustomerID, "other garage")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	advisor := business.AddStaff(t, "Service Advisor")
	_, err = advisor.API.ChangeVehicleOwner(ctx, business.GarageID, business.VehicleID, business.CustomerID, "back")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	history, err := api.GetVehicleHistory(ctx, business.GarageID, business.VehicleID)
	require.NoError(t, err)
	assert.NotEmpty(t, history, "the repair history follows the vehicle")

	vehicles, err := api.ListVehicles(ctx, business.GarageID)
	require.NoError(t, err)

	for _, v := range vehicles {
		if v.GetId() == business.VehicleID {
			assert.Equal(t, next.GetId(), v.GetCustomerId())
		}
	}
}

// Scenario: E2E-CUS-022: deleting asks what it will do first (dryRun) and
// then does the same: an unused record is deleted and its key freed, a used
// one is deactivated and keeps its key, and reactivating brings it back.
func TestDeleteOrDeactivate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "cus-lifecycle")
	api := business.Owner.API

	type kind struct {
		name   string
		remove func(id string, dryRun bool) (commonv1.DeleteOutcome, error)
	}

	customers := kind{"customer", func(id string, dryRun bool) (commonv1.DeleteOutcome, error) {
		return api.DeleteCustomerOutcome(ctx, business.GarageID, id, dryRun)
	}}
	vehicles := kind{"vehicle", func(id string, dryRun bool) (commonv1.DeleteOutcome, error) {
		return api.DeleteVehicleOutcome(ctx, business.GarageID, id, dryRun)
	}}
	suppliers := kind{"supplier", func(id string, dryRun bool) (commonv1.DeleteOutcome, error) {
		return api.DeleteSupplierOutcome(ctx, business.GarageID, id, dryRun)
	}}

	expect := func(t *testing.T, k kind, id string, want commonv1.DeleteOutcome) {
		t.Helper()

		preview, err := k.remove(id, true)
		require.NoError(t, err)
		assert.Equal(t, want, preview, "%s: the preview", k.name)

		done, err := k.remove(id, false)
		require.NoError(t, err)
		assert.Equal(t, want, done, "%s: the delete does what the preview said", k.name)
	}

	phoneUnused, phoneUsed := "0911000131", "0911000132"

	unusedCustomer, err := api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Unused", Phone: &phoneUnused})
	require.NoError(t, err)

	usedCustomer, err := api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Used", Phone: &phoneUsed})
	require.NoError(t, err)

	usedVehicle, err := api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: usedCustomer.GetId(), LicensePlate: "51A-13200"})
	require.NoError(t, err)

	unusedVehicle, err := api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: unusedCustomer.GetId(), LicensePlate: "51A-13100"})
	require.NoError(t, err)

	_, err = api.CreateQuote(ctx, business.GarageID, laborQuote(usedCustomer.GetId(), usedVehicle.GetId()))
	require.NoError(t, err)

	// Suppliers: one with a goods receipt would need stock set-up; an unused
	// one is deleted and a name freed. Used suppliers are covered by the
	// inventory scenarios.
	unusedSupplier, err := api.CreateSupplier(ctx, business.GarageID, &supplierv1.Supplier{Name: "QA Supplier Unused"})
	require.NoError(t, err)

	expect(t, suppliers, unusedSupplier.GetId(), commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED)

	_, err = api.CreateSupplier(ctx, business.GarageID, &supplierv1.Supplier{Name: "QA Supplier Unused"})
	require.NoError(t, err, "the name of a deleted supplier is free")

	expect(t, vehicles, unusedVehicle.GetId(), commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED)
	expect(t, customers, unusedCustomer.GetId(), commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED)

	_, err = api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Reborn", Phone: &phoneUnused})
	require.NoError(t, err, "the phone of a deleted customer is free")

	_, err = api.CreateVehicle(ctx, business.GarageID, &customerv1.Vehicle{CustomerId: business.CustomerID, LicensePlate: "51A-13100"})
	require.NoError(t, err, "the plate of a deleted vehicle is free")

	expect(t, customers, usedCustomer.GetId(), commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED)

	_, err = api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: "QA Customer Clash", Phone: &phoneUsed})
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_EXISTS")

	got, err := api.GetCustomer(ctx, business.GarageID, usedCustomer.GetId())
	require.NoError(t, err)
	assert.False(t, got.GetIsActive(), "still on file, inactive")

	back, err := api.ActivateCustomer(ctx, business.GarageID, usedCustomer.GetId())
	require.NoError(t, err)
	assert.True(t, back.GetIsActive())

	again, err := api.ActivateCustomer(ctx, business.GarageID, usedCustomer.GetId())
	require.NoError(t, err, "reactivating an active record changes nothing")
	assert.True(t, again.GetIsActive())

	expect(t, vehicles, usedVehicle.GetId(), commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED)

	revived, err := api.ActivateVehicle(ctx, business.GarageID, usedVehicle.GetId())
	require.NoError(t, err)
	assert.True(t, revived.GetIsActive())

	t.Run("a member without customer.write cannot delete or reactivate", func(t *testing.T) {
		technician := business.AddStaff(t, "Technician")

		_, err := technician.API.DeleteCustomerOutcome(ctx, business.GarageID, usedCustomer.GetId(), false)
		require.Error(t, err)
		assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, client.AsAPIError(err).Status)
	})
}
