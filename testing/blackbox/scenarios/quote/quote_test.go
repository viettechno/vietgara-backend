// Package quote_test covers Quotes & Pricing at API level: draft
// creation, validation, edit/delete guards, LINK approval with the
// e-mailed link, the unauthenticated public decision, MANUAL approval,
// conversion to a repair order, and listing/filtering. Quote PDFs, the
// daily limit and permission screens are covered in the reporting,
// license and tenant-adjacent tests; banners and copy are web-E2E.
package quote_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// setup returns a fresh owner, garage, customer and vehicle for quote
// scenarios.
func setup(t *testing.T, label, customerEmail string) (*fixture.Actor, *tenantv1.Garage, *customerv1.Customer, *customerv1.Vehicle) {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	// Zero-padded suffix keeps phones (10+ digits) and plates unique per
	// test run.
	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09110" + suffix

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, Email: &customerEmail, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51Q-" + suffix,
	})
	require.NoError(t, err)

	return owner, garage, customer, vehicle
}

func laborLine(description, quantity string, unitPrice int64) *quotev1.QuoteLine {
	return &quotev1.QuoteLine{
		ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: description, Quantity: quantity, UnitPrice: unitPrice,
	}
}

func partLine(description, quantity string, unitPrice int64) *quotev1.QuoteLine {
	return &quotev1.QuoteLine{
		ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, Description: description, Quantity: quantity, UnitPrice: unitPrice,
	}
}

// draftQuote creates a draft LINK-mode quote with one labor and one part
// line (total 970,000 VND).
func draftQuote(t *testing.T, api *client.Client, garageID, customerID, vehicleID string) *quotev1.Quote {
	t.Helper()

	quote, err := api.CreateQuote(t.Context(), garageID, &quotev1.Quote{
		CustomerId: customerID, VehicleId: vehicleID,
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{
			laborLine("Oil change service", "1", 250000),
			partLine("Engine oil 5W-30", "4", 180000),
		},
	})
	require.NoError(t, err)

	return quote
}

// tokenFromURL returns the public token at the end of a /public/quotes/
// URL.
func tokenFromURL(t *testing.T, publicURL string) string {
	t.Helper()

	parts := strings.Split(publicURL, "/public/quotes/")
	require.Len(t, parts, 2, "public URL %q", publicURL)
	require.NotEmpty(t, parts[1])

	return parts[1]
}

// decisions approves every line id when approve is true, rejects
// otherwise.
func decisions(quote *quotev1.Quote, approve bool) []*quotev1.QuoteLineDecisionInput {
	out := make([]*quotev1.QuoteLineDecisionInput, 0, len(quote.GetLines()))
	for _, line := range quote.GetLines() {
		out = append(out, &quotev1.QuoteLineDecisionInput{LineId: line.GetId(), Approved: approve})
	}

	return out
}

// Scenario: E2E-QUO-001 (partial): a draft quote is created with labor
// and part lines — the API substance (number, status, totals); the list
// badges and the garage filter are web-E2E.
func TestCreateDraftQuote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-create", fixture.UniqueEmail(t, "quo-create"))

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, quote.GetStatus())
	assert.NotEmpty(t, quote.GetNumber())
	assert.EqualValues(t, 250000, quote.GetLines()[0].GetLineTotal())
	assert.EqualValues(t, 720000, quote.GetLines()[1].GetLineTotal())
	assert.EqualValues(t, 970000, quote.GetTotalAmount())

	got, err := owner.API.GetQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quote.GetId(), got.GetId())
	assert.Equal(t, customer.GetId(), got.GetCustomerId())
	assert.Equal(t, vehicle.GetId(), got.GetVehicleId())
	assert.EqualValues(t, 970000, got.GetTotalAmount())

	listed, err := owner.API.ListQuotes(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, listed)
}

// Scenario: E2E-QUO-002 (partial): quote field validation — the API
// guards behind the form.
func TestQuoteValidation(t *testing.T) {
	t.Parallel()

	owner, garage, customer, vehicle := setup(t, "quo-valid", fixture.UniqueEmail(t, "quo-valid"))

	newQuote := func(lines ...*quotev1.QuoteLine) *quotev1.Quote {
		return &quotev1.Quote{CustomerId: customer.GetId(), VehicleId: vehicle.GetId(), Lines: lines}
	}

	// Empty description is refused.
	_, err := owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote(laborLine("", "1", 100000)))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Zero quantity is refused.
	_, err = owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote(laborLine("Brake pad", "0", 100000)))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Three decimals are refused.
	_, err = owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote(laborLine("Brake pad", "1.999", 100000)))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Negative unit price is refused.
	_, err = owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote(laborLine("Brake pad", "1", -1)))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// No lines is refused.
	_, err = owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Fractional quantity is saved.
	saved, err := owner.API.CreateQuote(t.Context(), garage.GetId(), newQuote(laborLine("Brake pad", "1.5", 100000)))
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteQuote(t.Context(), garage.GetId(), saved.GetId()))
}

// Scenario: E2E-QUO-003 (partial): a draft is edited and deleted, and a
// non-draft quote can be neither — the API guards; the editor controls
// are web-E2E.
func TestEditAndDeleteDraft(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-edit", fixture.UniqueEmail(t, "quo-edit"))

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	updated, err := owner.API.UpdateQuote(ctx, garage.GetId(), quote.GetId(), &quotev1.Quote{
		Lines: []*quotev1.QuoteLine{
			laborLine("Oil change service", "1", 280000),
			partLine("Engine oil 5W-30", "4", 180000),
			laborLine("Car wash", "1", 50000),
		},
	}, "lines")
	require.NoError(t, err)
	assert.EqualValues(t, 280000+720000+50000, updated.GetTotalAmount())

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, sent.GetQuote().GetStatus())

	// Editing a sent quote is refused.
	_, err = owner.API.UpdateQuote(ctx, garage.GetId(), quote.GetId(), &quotev1.Quote{
		Lines: []*quotev1.QuoteLine{laborLine("Oil change service", "1", 1)},
	}, "lines")
	fixture.RequireAPIError(t, err, http.StatusPreconditionFailed, "")

	// Deleting a sent quote is refused.
	err = owner.API.DeleteQuote(ctx, garage.GetId(), quote.GetId())
	require.Error(t, err)

	// A second, still-draft quote deletes cleanly.
	other := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())
	require.NoError(t, owner.API.DeleteQuote(ctx, garage.GetId(), other.GetId()))

	_, err = owner.API.GetQuote(ctx, garage.GetId(), other.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}

// Scenario: E2E-QUO-004 (partial): sending for approval in LINK mode
// e-mails the link when the customer has an e-mail on file — asserted
// at API level plus the test inbox; the banner is web-E2E.
func TestSendLinkModeWithEmail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	email := fixture.UniqueEmail(t, "quo-link")
	owner, garage, customer, vehicle := setup(t, "quo-link", email)
	mail := fixture.Mailpit(t)

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, sent.GetQuote().GetStatus())
	assert.True(t, sent.GetEmailed())
	assert.Contains(t, sent.GetPublicUrl(), "/public/quotes/")

	msg, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	assert.Contains(t, msg.Text, "/public/quotes/"+tokenFromURL(t, sent.GetPublicUrl()))
	assert.Contains(t, msg.Text, sent.GetQuote().GetNumber())
}

// Scenario: E2E-QUO-005 (partial): LINK mode without an e-mail on file
// still creates the link for the advisor to copy — asserted at API
// level; copy-paste is web-E2E.
func TestSendLinkModeWithoutEmail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, _, _ := setup(t, "quo-nomail", fixture.UniqueEmail(t, "quo-nomail-unused"))

	phone := "0911000521"

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer No Mail", Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51Q-NM1",
	})
	require.NoError(t, err)

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.False(t, sent.GetEmailed())
	assert.Contains(t, sent.GetPublicUrl(), "/public/quotes/")

	public, err := fixture.Anonymous(t).GetPublicQuote(ctx, tokenFromURL(t, sent.GetPublicUrl()))
	require.NoError(t, err)
	assert.Equal(t, sent.GetQuote().GetNumber(), public.GetNumber())
	assert.Equal(t, customer.GetFullName(), public.GetCustomerName())
}

// Scenario: E2E-QUO-006 (partial): the customer decides each line from
// the public link — asserted at API level without signing in; the
// public page itself is web-E2E.
func TestPublicLinkDecision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-public", fixture.UniqueEmail(t, "quo-public"))

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	token := tokenFromURL(t, sent.GetPublicUrl())
	anon := fixture.Anonymous(t)

	public, err := anon.GetPublicQuote(ctx, token)
	require.NoError(t, err)
	require.Len(t, public.GetLines(), 2)
	assert.Equal(t, quote.GetNumber(), public.GetNumber())
	assert.Equal(t, customer.GetFullName(), public.GetCustomerName())

	// Approve the first line, reject the second.
	mixed, err := anon.DecidePublicQuote(ctx, token,
		&quotev1.QuoteLineDecisionInput{LineId: public.GetLines()[0].GetId(), Approved: true},
		&quotev1.QuoteLineDecisionInput{LineId: public.GetLines()[1].GetId(), Approved: false},
	)
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, mixed.GetStatus())

	stored, err := owner.API.GetQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, stored.GetStatus())
	assert.Equal(t, quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_APPROVED, stored.GetLines()[0].GetDecision())
	assert.Equal(t, quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_REJECTED, stored.GetLines()[1].GetDecision())

	// A fresh sent quote where every line is rejected becomes Rejected.
	quote2 := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	sent2, err := owner.API.SendQuote(ctx, garage.GetId(), quote2.GetId())
	require.NoError(t, err)

	token2 := tokenFromURL(t, sent2.GetPublicUrl())

	public2, err := anon.GetPublicQuote(ctx, token2)
	require.NoError(t, err)

	rejected, err := anon.DecidePublicQuote(ctx, token2,
		&quotev1.QuoteLineDecisionInput{LineId: public2.GetLines()[0].GetId(), Approved: false},
		&quotev1.QuoteLineDecisionInput{LineId: public2.GetLines()[1].GetId(), Approved: false},
	)
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_REJECTED, rejected.GetStatus())

	stored2, err := owner.API.GetQuote(ctx, garage.GetId(), quote2.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_REJECTED, stored2.GetStatus())
}

// Scenario: E2E-QUO-007 (partial): the public link stops working once
// decided or unknown — asserted at API level (422 QUOTE_LINK_INVALID).
func TestPublicLinkInvalid(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-badlink", fixture.UniqueEmail(t, "quo-badlink"))
	anon := fixture.Anonymous(t)

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	token := tokenFromURL(t, sent.GetPublicUrl())

	_, err = anon.DecidePublicQuote(ctx, token, decisions(sent.GetQuote(), true)...)
	require.NoError(t, err)

	// Deciding again is refused.
	_, err = anon.DecidePublicQuote(ctx, token, decisions(sent.GetQuote(), true)...)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "QUOTE_LINK_INVALID")

	// An unknown token is refused.
	_, err = anon.GetPublicQuote(ctx, "not-a-real-token")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "QUOTE_LINK_INVALID")

	_, err = anon.DecidePublicQuote(ctx, "not-a-real-token", decisions(sent.GetQuote(), true)...)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "QUOTE_LINK_INVALID")
}

// Scenario: E2E-QUO-008 (partial): MANUAL mode — the advisor records the
// customer's decision directly — asserted at API level.
func TestManualDecision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-manual", fixture.UniqueEmail(t, "quo-manual"))

	quote, err := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
		Lines: []*quotev1.QuoteLine{
			laborLine("Oil change service", "1", 250000),
			partLine("Engine oil 5W-30", "4", 180000),
		},
	})
	require.NoError(t, err)

	sent, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, sent.GetQuote().GetStatus())
	assert.False(t, sent.GetEmailed())
	assert.Empty(t, sent.GetPublicUrl(), "MANUAL mode has no public link")

	decided, err := owner.API.RecordQuoteDecision(ctx, garage.GetId(), quote.GetId(), decisions(quote, true)...)
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())
}

// Scenario: E2E-QUO-010 (partial): an approved quote converts into a
// repair order with only the approved lines — the API substance; the
// banner and vehicle history entry are web-E2E (history is covered in
// the customer package).
func TestConvertApprovedQuote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-convert", fixture.UniqueEmail(t, "quo-convert"))

	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	_, err := owner.API.RecordQuoteDecision(ctx, garage.GetId(), quote.GetId(),
		&quotev1.QuoteLineDecisionInput{LineId: quote.GetLines()[0].GetId(), Approved: true},
		&quotev1.QuoteLineDecisionInput{LineId: quote.GetLines()[1].GetId(), Approved: false},
	)
	require.NoError(t, err)

	converted, err := owner.API.ConvertQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_CONVERTED, converted.GetQuote().GetStatus())
	assert.NotEmpty(t, converted.GetRepairOrderId())

	order, err := owner.API.GetRepairOrder(ctx, garage.GetId(), converted.GetRepairOrderId())
	require.NoError(t, err)
	require.Len(t, order.GetLines(), 1, "only the approved line converts")
	assert.Equal(t, "Oil change service", order.GetLines()[0].GetDescription())
	assert.Equal(t, quote.GetId(), order.GetQuoteId())

	// Converting again is refused.
	_, err = owner.API.ConvertQuote(ctx, garage.GetId(), quote.GetId())
	fixture.RequireAPIError(t, err, http.StatusPreconditionFailed, "")
}

// Scenario: E2E-QUO-013 (partial): quotes list and filter by status —
// the API substance; the filter controls are web-E2E.
func TestListAndFilterQuotes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-list", fixture.UniqueEmail(t, "quo-list"))

	draft := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	toSend := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	_, err := owner.API.SendQuote(ctx, garage.GetId(), toSend.GetId())
	require.NoError(t, err)

	all, err := owner.API.ListQuotes(ctx, garage.GetId())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)

	drafts, err := owner.API.ListQuotesByStatus(ctx, garage.GetId(), quotev1.QuoteStatus_QUOTE_STATUS_DRAFT)
	require.NoError(t, err)
	require.NotEmpty(t, drafts)

	for _, q := range drafts {
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, q.GetStatus())
	}

	sent, err := owner.API.ListQuotesByStatus(ctx, garage.GetId(), quotev1.QuoteStatus_QUOTE_STATUS_SENT)
	require.NoError(t, err)
	require.NotEmpty(t, sent)

	for _, q := range sent {
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, q.GetStatus())
	}

	assert.NotEmpty(t, draft.GetId())
}
