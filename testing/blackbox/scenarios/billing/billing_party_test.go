// Release 5.1 on the settlement: who is billed, whether a VAT invoice is
// requested, and the voucher rules around a void.
package billing_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func company(legalName string, taxID, address *string) *customerv1.CustomerTaxProfile {
	return &customerv1.CustomerTaxProfile{
		Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: legalName, TaxId: taxID, Address: address,
	}
}

// Scenario: E2E-BIL-011: a VAT invoice can be requested only when the
// billing party has a legal name, a tax ID and an address; a retail receipt
// never can; only the accountant side may change it.
func TestInvoiceRequestNeedsACompleteBillingParty(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "bil-invoice")
	api := business.Owner.API

	incomplete, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID, company("QA Company Incomplete", nil, nil))
	require.NoError(t, err)

	settlement := business.SettlementBilledTo(t, incomplete.GetId())
	assert.Equal(t, "QA Company Incomplete", settlement.GetBillingParty().GetLegalName())
	assert.False(t, settlement.GetInvoiceRequested(), "a settlement starts as a retail receipt")

	request := func(taxProfileID string, invoice bool) (*billingv1.Settlement, error) {
		return api.UpdateSettlement(ctx, business.GarageID, settlement.GetId(), &billingv1.Settlement{
			BillingParty: &commonv1.BillingParty{TaxProfileId: taxProfileID}, InvoiceRequested: invoice,
		}, "billing_party,invoice_requested")
	}

	_, err = request(incomplete.GetId(), true)
	fixture.RequireAPIError(t, err, http.StatusConflict, "INVOICE_DATA_INCOMPLETE")

	// Complete the profile; the settlement picks the new data when the party
	// is chosen again.
	_, err = api.UpdateTaxProfile(ctx, business.GarageID, business.CustomerID, incomplete.GetId(),
		&customerv1.CustomerTaxProfile{TaxId: new("0312345678"), Address: new("1 Nguyen Hue, Quan 1")}, "tax_id,address")
	require.NoError(t, err)

	invoiced, err := request(incomplete.GetId(), true)
	require.NoError(t, err)
	assert.True(t, invoiced.GetInvoiceRequested())
	assert.Equal(t, "0312345678", invoiced.GetBillingParty().GetTaxId())
	assert.Equal(t, "1 Nguyen Hue, Quan 1", invoiced.GetBillingParty().GetAddress())

	_, err = request("", true)
	fixture.RequireAPIError(t, err, http.StatusConflict, "INVOICE_DATA_INCOMPLETE")

	retail, err := request("", false)
	require.NoError(t, err)
	assert.False(t, retail.GetInvoiceRequested())
	assert.Empty(t, retail.GetBillingParty().GetTaxProfileId(), "a retail receipt has no profile")

	technician := business.AddStaff(t, "Technician")
	_, err = technician.API.UpdateSettlement(ctx, business.GarageID, settlement.GetId(),
		&billingv1.Settlement{InvoiceRequested: true}, "invoice_requested")
	require.Error(t, err, "a technician has no billing access")

	_, err = api.UpdateSettlement(ctx, business.GarageID, settlement.GetId(), &billingv1.Settlement{TotalAmount: 1}, "total_amount")
	require.Error(t, err, "only the billing party and the invoice flag can change")
}

// Scenario: E2E-BIL-012: the billing party is a snapshot: editing the
// profile afterwards changes neither the settlement nor the quote it came
// from, only what is made next.
func TestBillingPartyIsASnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "bil-snapshot")
	api := business.Owner.API

	profile, err := api.CreateTaxProfile(ctx, business.GarageID, business.CustomerID,
		company("QA Company Hoa Binh JSC", new("0312345678"), new("1 Nguyen Hue, Quan 1")))
	require.NoError(t, err)

	settlement := business.SettlementBilledTo(t, profile.GetId())
	require.Equal(t, "QA Company Hoa Binh JSC", settlement.GetBillingParty().GetLegalName())

	_, err = api.UpdateTaxProfile(ctx, business.GarageID, business.CustomerID, profile.GetId(),
		&customerv1.CustomerTaxProfile{LegalName: "QA Company Hoa Binh Holdings", Address: new("2 Le Loi, Quan 1")}, "legal_name,address")
	require.NoError(t, err)

	got, err := api.GetSettlement(ctx, business.GarageID, settlement.GetId())
	require.NoError(t, err)
	assert.Equal(t, "QA Company Hoa Binh JSC", got.GetBillingParty().GetLegalName(), "the issued settlement keeps what it was billed to")
	assert.Equal(t, "1 Nguyen Hue, Quan 1", got.GetBillingParty().GetAddress())

	next := business.SettlementBilledTo(t, profile.GetId())
	assert.Equal(t, "QA Company Hoa Binh Holdings", next.GetBillingParty().GetLegalName(), "the next document starts from the edited profile")
}

// Scenario: E2E-BIL-013: a void needs a reason, works once, takes an
// unknown payment as not found, frees the room for a new payment with the
// next voucher number, and is a billing-write action.
func TestVoidEdgeCases(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "bil-void")
	api := business.Owner.API
	settlement := business.Settlement(t)

	pay := func(amount int64) *billingv1.Settlement {
		paid, err := api.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, business.GarageID, settlement.GetId(),
			&billingv1.RecordSettlementPaymentRequest{Amount: amount, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
		require.NoError(t, err)

		return paid
	}

	paid := pay(fixture.LaborTotal)
	require.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, paid.GetStatus())

	first := paid.GetPayments()[0]

	_, err := api.VoidSettlementPayment(ctx, business.GarageID, settlement.GetId(), first.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = api.VoidSettlementPayment(ctx, business.GarageID, settlement.GetId(), uuid.NewString(), "Typed twice")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	technician := business.AddStaff(t, "Technician")
	_, err = technician.API.VoidSettlementPayment(ctx, business.GarageID, settlement.GetId(), first.GetId(), "Not mine")
	require.Error(t, err)

	voided, err := api.VoidSettlementPayment(ctx, business.GarageID, settlement.GetId(), first.GetId(), "Typed twice")
	require.NoError(t, err)
	assert.EqualValues(t, 0, voided.GetPaidAmount())
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING, voided.GetStatus(), "the only payment is gone")

	_, err = api.VoidSettlementPayment(ctx, business.GarageID, settlement.GetId(), first.GetId(), "Again")
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_EXISTS")

	again := pay(fixture.LaborTotal)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, again.GetStatus())
	require.Len(t, again.GetPayments(), 2, "the voided voucher stays on file")

	var live *billingv1.SettlementPayment

	for _, payment := range again.GetPayments() {
		if !payment.GetVoided() {
			live = payment
		}
	}

	require.NotNil(t, live)
	assert.NotEqual(t, first.GetVoucherNumber(), live.GetVoucherNumber(), "a voided number is never reused")
}
