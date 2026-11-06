// Scenario: E2E-DSR-001 to E2E-DSR-009
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/18_Data_Requests.md
package customer_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

const anonymizedName = "Khách hàng đã ẩn danh"

var tokenPattern = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

// dsrKit is a garage whose customer has an address on the test domain, a
// vehicle, a closed repair order, a settlement with a payment and a tax profile.
type dsrKit struct {
	*fixture.Business

	email      string
	plate      string
	settlement *billingv1.Settlement
}

func newDSRKit(t *testing.T, label string) *dsrKit {
	t.Helper()

	ctx := t.Context()
	b := fixture.NewBusiness(t, label)
	k := &dsrKit{Business: b, email: fixture.UniqueEmail(t, label)}

	_, err := b.Owner.API.UpdateCustomer(ctx, b.GarageID, b.CustomerID, &customerv1.Customer{Email: &k.email, PersonalDataConsent: true}, "email", "personal_data_consent")
	require.NoError(t, err)

	vehicle, err := b.Owner.API.GetVehicle(ctx, b.GarageID, b.VehicleID)
	require.NoError(t, err)

	k.plate = vehicle.GetLicensePlate()
	k.settlement = b.Settlement(t)

	_, err = b.Owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, b.GarageID, k.settlement.GetId(),
		&billingv1.RecordSettlementPaymentRequest{Amount: fixture.LaborTotal / 2, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
	require.NoError(t, err)

	taxID := "0301234567"
	_, err = b.Owner.API.CreateTaxProfile(ctx, b.GarageID, b.CustomerID, &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "Cong ty An", TaxId: &taxID, Address: new("1 Le Loi"),
	})
	require.NoError(t, err)

	return k
}

// Scenario: E2E-DSR-001: a view recorded by staff is answered with the data held and nothing is stored.
func TestDataRequestViewIsAnsweredAndNotStored(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newDSRKit(t, "dsr-view")
	advisor := k.AddStaff(t, "Service Advisor")

	got, err := advisor.API.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, "")
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED, got.GetStatus())
	assert.Equal(t, customerv1.DataRequestSource_DATA_REQUEST_SOURCE_STAFF, got.GetSource())

	for _, want := range []string{k.email, k.plate, "0301234567", k.settlement.GetNumber(), "QA Customer"} {
		assert.Contains(t, got.GetExportJson(), want)
	}

	list, err := advisor.API.ListDataRequests(ctx, k.GarageID, url.Values{"customerId": {k.CustomerID}})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.Empty(t, list.GetData()[0].GetExportJson(), "no copy of the data is stored")

	one, err := advisor.API.GetDataRequest(ctx, k.GarageID, got.GetId())
	require.NoError(t, err)
	assert.Empty(t, one.GetExportJson())
	assert.Equal(t, customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, one.GetType())
	assert.NotNil(t, one.GetRequestedAt())
}

// Scenario: E2E-DSR-002: a deletion anonymizes the customer, keeps the financial documents,
// and reaches the receivable and the notification log.
func TestDataRequestDeleteAnonymizesAndKeepsTheDocuments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newDSRKit(t, "dsr-delete")
	api := k.Owner.API

	// The receivable follows the settlement through the outbox.
	waitFor(t, "the customer's receivable", func() bool {
		list, err := api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{CustomerID: k.CustomerID, IncludePaid: true})

		return err == nil && len(list.GetData()) == 1
	})

	done, err := api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "")
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED, done.GetStatus())
	assert.Equal(t, "Anonymized 1 customer, 1 vehicle(s) and 1 tax profile(s)", done.GetOutcome())

	c, err := api.GetCustomer(ctx, k.GarageID, k.CustomerID)
	require.NoError(t, err)
	assert.Equal(t, anonymizedName, c.GetFullName())
	assert.Empty(t, c.GetPhone())
	assert.Empty(t, c.GetEmail())
	assert.Empty(t, c.GetAddress())

	v, err := api.GetVehicle(ctx, k.GarageID, k.VehicleID)
	require.NoError(t, err)
	assert.NotEqual(t, k.plate, v.GetLicensePlate())
	assert.True(t, strings.HasPrefix(v.GetLicensePlate(), "ANON-"))
	assert.Empty(t, v.GetVin())
	assert.Empty(t, v.GetEngineNumber())

	profiles, err := api.ListTaxProfiles(ctx, k.GarageID, k.CustomerID, true)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Empty(t, profiles[0].GetTaxId())
	assert.Empty(t, profiles[0].GetAddress())

	// The documents are unchanged: the amounts and the data of when they were issued.
	settlement, err := api.GetSettlement(ctx, k.GarageID, k.settlement.GetId())
	require.NoError(t, err)
	assert.Equal(t, k.settlement.GetTotalAmount(), settlement.GetTotalAmount())
	assert.Equal(t, k.settlement.GetCustomerName(), settlement.GetCustomerName())
	assert.Equal(t, k.plate, settlement.GetVehicleLicensePlate())
	require.Len(t, settlement.GetPayments(), 1)

	// The receivable keeps its amounts and loses the personal data.
	waitFor(t, "the receivable to follow the anonymization", func() bool {
		list, listErr := api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{CustomerID: k.CustomerID, IncludePaid: true})

		return listErr == nil && len(list.GetData()) == 1 && list.GetData()[0].GetCustomerName() == anonymizedName
	})

	list, err := api.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{CustomerID: k.CustomerID, IncludePaid: true})
	require.NoError(t, err)
	assert.Equal(t, "ANON", list.GetData()[0].GetVehicleLicensePlate())
	assert.Equal(t, k.settlement.GetTotalAmount(), list.GetData()[0].GetTotalAmount())

	// The customer cannot be edited, found by its old phone, e-mail or plate, or given a vehicle.
	_, err = api.UpdateCustomer(ctx, k.GarageID, k.CustomerID, &customerv1.Customer{FullName: "Again"}, "full_name")
	fixture.RequireAPIError(t, err, http.StatusConflict, "CUSTOMER_ANONYMIZED")

	found, err := api.LookupCustomers(ctx, k.GarageID, "", k.email)
	require.NoError(t, err)
	assert.Empty(t, found.GetData())

	_, err = api.CreateVehicle(ctx, k.GarageID, &customerv1.Vehicle{CustomerId: k.CustomerID, LicensePlate: "51B-999.99"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "CUSTOMER_ANONYMIZED")

	// The erased vehicle and tax profile cannot be given their data back.
	_, err = api.UpdateVehicle(ctx, k.GarageID, k.VehicleID, &customerv1.Vehicle{Vin: new("REAL")}, "vin")
	require.Error(t, err)

	again, err := api.GetVehicle(ctx, k.GarageID, k.VehicleID)
	require.NoError(t, err)
	assert.Empty(t, again.GetVin())

	// A second deletion is refused; the record of the first holds no personal data.
	_, err = api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "")
	fixture.RequireAPIError(t, err, http.StatusConflict, "CUSTOMER_ANONYMIZED")

	kept, err := api.GetDataRequest(ctx, k.GarageID, done.GetId())
	require.NoError(t, err)
	assert.Empty(t, kept.GetDetails())
}

// Scenario: E2E-DSR-003: a correction waits for the garage, which completes or declines it;
// anonymizing the customer clears the text of every request about them.
func TestDataRequestCorrectionIsCompletedOrDeclined(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newDSRKit(t, "dsr-correct")
	api := k.Owner.API
	correct := customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT

	_, err := api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, correct, "  ")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	first, err := api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, correct, "My phone number is 0911000999")
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_PENDING, first.GetStatus())
	assert.Equal(t, "My phone number is 0911000999", first.GetDetails())

	phone := "0911000999"
	_, err = api.UpdateCustomer(ctx, k.GarageID, k.CustomerID, &customerv1.Customer{Phone: &phone}, "phone")
	require.NoError(t, err)

	done, err := api.CompleteDataRequest(ctx, k.GarageID, first.GetId(), "Phone changed", false)
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED, done.GetStatus())
	assert.Equal(t, "Phone changed", done.GetOutcome())
	assert.Equal(t, k.Owner.Session.GetUser().GetFullName(), done.GetCompletedByName())

	second, err := api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, correct, "Wrong plate")
	require.NoError(t, err)

	declined, err := api.CompleteDataRequest(ctx, k.GarageID, second.GetId(), "The plate is on a live document", true)
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_REJECTED, declined.GetStatus())
	assert.Equal(t, "The plate is on a live document", declined.GetOutcome())

	_, err = api.CompleteDataRequest(ctx, k.GarageID, first.GetId(), "Again", false)
	fixture.RequireAPIError(t, err, http.StatusConflict, "DATA_REQUEST_NOT_PENDING")

	_, err = api.CompleteDataRequest(ctx, k.GarageID, second.GetId(), "", false)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	pending, err := api.ListDataRequests(ctx, k.GarageID, url.Values{"status": {"DATA_REQUEST_STATUS_PENDING"}})
	require.NoError(t, err)
	assert.Empty(t, pending.GetData())

	// The text of a correction does not outlive the customer's data.
	_, err = api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "")
	require.NoError(t, err)

	cleared, err := api.GetDataRequest(ctx, k.GarageID, first.GetId())
	require.NoError(t, err)
	assert.Empty(t, cleared.GetDetails())
	assert.Equal(t, "Cleared when the customer was anonymized", cleared.GetOutcome(), "a correction's outcome may repeat what was corrected")
}

// publicKit is two garages whose customers share one address.
type publicKit struct {
	one, two *fixture.Business
	email    string
	inbox    *client.Mailpit
	api      *client.Client
}

func newPublicKit(t *testing.T, label string) *publicKit {
	t.Helper()

	k := &publicKit{
		one: fixture.NewBusiness(t, label+"-a"), two: fixture.NewBusiness(t, label+"-b"), email: fixture.UniqueEmail(t, label),
		inbox: fixture.Mailpit(t), api: fixture.Anonymous(t),
	}

	for _, b := range []*fixture.Business{k.one, k.two} {
		_, err := b.Owner.API.UpdateCustomer(t.Context(), b.GarageID, b.CustomerID, &customerv1.Customer{Email: &k.email, PersonalDataConsent: true}, "email", "personal_data_consent")
		require.NoError(t, err)
	}

	return k
}

// token waits for the confirmation e-mail with the subject fragment and returns its token.
func (k *publicKit) token(t *testing.T, address, subject string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	msg, err := k.inbox.WaitForMatching(ctx, address, func(m *client.Message) bool { return strings.Contains(m.Subject, subject) })
	require.NoError(t, err)

	match := tokenPattern.FindStringSubmatch(msg.Text)
	require.Len(t, match, 2, msg.Text)

	return match[1]
}

// Scenario: E2E-DSR-004: a deletion confirmed by e-mail reaches every garage that holds the address.
func TestPublicDeleteReachesEveryGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newPublicKit(t, "dsr-public")
	bystander := fixture.NewBusiness(t, "dsr-bystander")

	_, err := k.api.SubmitPublicDataRequest(ctx, k.email, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "", "en")
	require.NoError(t, err)

	token := k.token(t, k.email, "Confirm")

	resp, err := k.api.ConfirmPublicDataRequest(ctx, token)
	require.NoError(t, err)
	assert.EqualValues(t, 2, resp.GetGarageCount())

	for _, b := range []*fixture.Business{k.one, k.two} {
		c, getErr := b.Owner.API.GetCustomer(ctx, b.GarageID, b.CustomerID)
		require.NoError(t, getErr)
		assert.Equal(t, anonymizedName, c.GetFullName())
		assert.Empty(t, c.GetEmail())

		list, listErr := b.Owner.API.ListDataRequests(ctx, b.GarageID, url.Values{"customerId": {b.CustomerID}})
		require.NoError(t, listErr)
		require.Len(t, list.GetData(), 1)
		assert.Equal(t, customerv1.DataRequestSource_DATA_REQUEST_SOURCE_PUBLIC, list.GetData()[0].GetSource())
		assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED, list.GetData()[0].GetStatus())
	}

	other, err := bystander.Owner.API.GetCustomer(ctx, bystander.GarageID, bystander.CustomerID)
	require.NoError(t, err)
	assert.NotEqual(t, anonymizedName, other.GetFullName(), "customers with other addresses are untouched")
}

// Scenario: E2E-DSR-005: the request looks the same for a known and an unknown address, and bad input is refused first.
func TestPublicRequestLooksTheSameForAnyAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newPublicKit(t, "dsr-same")
	unknown := fixture.UniqueEmail(t, "dsr-nobody")
	types := []customerv1.DataRequestType{
		customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE,
	}

	for _, typ := range types {
		for _, address := range []string{k.email, unknown} {
			// Each submission comes from its own source, so only the answer is compared.
			resp, err := k.api.SubmitPublicDataRequest(ctx, address, typ, "", "")
			require.NoError(t, err)
			assert.Empty(t, resp.String())
		}
	}

	// Both addresses get the e-mail; the unknown address's owner learns that no garage holds it.
	_ = k.token(t, k.email, "Xác nhận")

	token := k.token(t, unknown, "Xác nhận")

	resp, err := k.api.ConfirmPublicDataRequest(ctx, token)
	require.NoError(t, err)
	assert.Zero(t, resp.GetGarageCount())

	for name, args := range map[string]struct {
		email, details string
		typ            customerv1.DataRequestType
	}{
		"malformed address":        {"not an address", "", customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW},
		"no address":               {"", "", customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW},
		"correction without text":  {k.email, "", customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT},
		"no type":                  {k.email, "", customerv1.DataRequestType_DATA_REQUEST_TYPE_UNSPECIFIED},
		"correction text too long": {k.email, strings.Repeat("x", 2001), customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT},
	} {
		_, err = k.api.SubmitPublicDataRequest(ctx, args.email, args.typ, args.details, "")
		require.Error(t, err, name)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	}
}

// Scenario: E2E-DSR-006: the confirmation token works once, in time and only in the body.
func TestPublicConfirmationTokenWorksOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newPublicKit(t, "dsr-token")

	_, err := k.api.SubmitPublicDataRequest(ctx, k.email, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "", "en")
	require.NoError(t, err)

	token := k.token(t, k.email, "Confirm")

	for name, tok := range map[string]string{"wrong": "not-the-token", "empty": ""} {
		_, err = k.api.ConfirmPublicDataRequest(ctx, tok)
		fixture.RequireAPIError(t, err, http.StatusNotFound, "NOT_FOUND")

		_ = name
	}

	probe, err := k.api.Probe(ctx, http.MethodPost, "/api/v1/public/data-requests/"+token+":confirm", []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, probe.Status, "the token is not accepted in the path")

	c, err := k.one.Owner.API.GetCustomer(ctx, k.one.GarageID, k.one.CustomerID)
	require.NoError(t, err)
	assert.NotEqual(t, anonymizedName, c.GetFullName(), "nothing changed so far")

	resp, err := k.api.ConfirmPublicDataRequest(ctx, token)
	require.NoError(t, err)
	assert.EqualValues(t, 2, resp.GetGarageCount())

	_, err = k.api.ConfirmPublicDataRequest(ctx, token)
	fixture.RequireAPIError(t, err, http.StatusNotFound, "NOT_FOUND")

	// A link that has expired does nothing either.
	late := fixture.UniqueEmail(t, "dsr-late")

	_, err = k.api.SubmitPublicDataRequest(ctx, late, customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, "", "en")
	require.NoError(t, err)

	lateToken := k.token(t, late, "Confirm")

	timeshift.ExpireDataRequestLink(t, late)

	_, err = k.api.ConfirmPublicDataRequest(ctx, lateToken)
	fixture.RequireAPIError(t, err, http.StatusNotFound, "NOT_FOUND")
}

// Scenario: E2E-DSR-007: a view is e-mailed to the confirmed address and nothing of it is stored.
func TestPublicViewIsEmailedToTheAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newPublicKit(t, "dsr-mailview")

	_, err := k.api.SubmitPublicDataRequest(ctx, k.email, customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, "", "en")
	require.NoError(t, err)

	resp, err := k.api.ConfirmPublicDataRequest(ctx, k.token(t, k.email, "Confirm"))
	require.NoError(t, err)
	assert.EqualValues(t, 2, resp.GetGarageCount())

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msg, err := k.inbox.WaitForMatching(waitCtx, k.email, func(m *client.Message) bool { return strings.Contains(m.Subject, "Your personal data") })
	require.NoError(t, err)
	assert.Contains(t, msg.Text, k.email)
	assert.Contains(t, msg.Text, "Blackbox dsr-mailview-a")
	assert.Contains(t, msg.Text, "Blackbox dsr-mailview-b")

	for _, b := range []*fixture.Business{k.one, k.two} {
		list, listErr := b.Owner.API.ListDataRequests(ctx, b.GarageID, url.Values{"customerId": {b.CustomerID}})
		require.NoError(t, listErr)
		require.Len(t, list.GetData(), 1)
		assert.Equal(t, customerv1.DataRequestSource_DATA_REQUEST_SOURCE_PUBLIC, list.GetData()[0].GetSource())
		assert.Empty(t, list.GetData()[0].GetExportJson())
	}
}

// Scenario: E2E-DSR-008: the public requests are throttled per address and per source.
func TestPublicRequestsAreThrottled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	inbox := fixture.Mailpit(t)
	address := fixture.UniqueEmail(t, "dsr-throttle")
	view := customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW

	// Per address: from three different sources, the fourth is refused.
	for i := range 3 {
		_, err := fixture.Anonymous(t).WithClientIP("198.51.100."+string(rune('1'+i))).SubmitPublicDataRequest(ctx, address, view, "", "en")
		require.NoError(t, err)
	}

	_, err := fixture.Anonymous(t).WithClientIP("198.51.100.9").SubmitPublicDataRequest(ctx, address, view, "", "en")
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "")

	count, err := inbox.Count(ctx, address, "Confirm")
	require.NoError(t, err)
	assert.LessOrEqual(t, count, 3, "no e-mail goes out for a refused request")

	// Per source: one source asking for many addresses is stopped after ten.
	source := fixture.Anonymous(t).WithClientIP("203.0.113." + string(rune('1'+time.Now().Second()%8)))

	var refused bool

	for range 12 {
		_, err = source.SubmitPublicDataRequest(ctx, fixture.UniqueEmail(t, "dsr-source"), view, "", "en")
		if err != nil {
			fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "")

			refused = true

			break
		}
	}

	assert.True(t, refused, "a source cannot ask for more than ten addresses an hour")
}

// Scenario: E2E-DSR-009: reading needs customer.read, writing customer.write, and another garage's requests are out of reach.
func TestDataRequestPermissionsAndIsolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newDSRKit(t, "dsr-perms")
	api := k.Owner.API

	role, err := api.CreateRole(ctx, &tenantv1.Role{Name: "DSR viewer " + k.email, Permissions: []tenantv1.Permission{tenantv1.Permission_PERMISSION_CUSTOMER_READ}})
	require.NoError(t, err)

	group, err := api.CreateStaffGroup(ctx, k.GarageID, &tenantv1.StaffGroup{Name: "DSR viewers " + k.email, RoleId: role.GetId()})
	require.NoError(t, err)

	viewer := k.AddStaff(t, group.GetName())
	technician := k.AddStaff(t, "Technician")
	foreign := fixture.NewBusiness(t, "dsr-foreign")

	made, err := api.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT, "x")
	require.NoError(t, err)

	_, err = viewer.API.ListDataRequests(ctx, k.GarageID, nil)
	require.NoError(t, err)

	_, err = viewer.API.GetDataRequest(ctx, k.GarageID, made.GetId())
	require.NoError(t, err)

	_, err = viewer.API.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW, "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = viewer.API.CompleteDataRequest(ctx, k.GarageID, made.GetId(), "done", false)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = technician.API.ListDataRequests(ctx, k.GarageID, nil)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = foreign.Owner.API.GetDataRequest(ctx, foreign.GarageID, made.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	_, err = foreign.Owner.API.GetDataRequest(ctx, k.GarageID, made.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = foreign.Owner.API.CreateDataRequest(ctx, k.GarageID, k.CustomerID, customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE, "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	c, err := api.GetCustomer(ctx, k.GarageID, k.CustomerID)
	require.NoError(t, err)
	assert.NotEqual(t, anonymizedName, c.GetFullName(), "the refused deletion changed nothing")
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(250 * time.Millisecond)
	}
}
