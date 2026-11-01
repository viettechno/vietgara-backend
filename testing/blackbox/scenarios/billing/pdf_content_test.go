package billing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// legalProfile is the garage identity the documents print in their header.
func legalProfile(t *testing.T, business *fixture.Business) {
	t.Helper()

	_, err := business.Owner.API.UpdateGarage(t.Context(), business.GarageID, &tenantv1.Garage{
		TaxCode:        new("0312345601"),
		LegalName:      new("Công ty TNHH Gara Ánh Dương"),
		LegalAddress:   new("12 Lê Lợi, Bến Nghé, Quận 1"),
		Representative: new("Nguyễn Văn Ánh"),
		Bank:           &commonv1.BankDetails{BankName: new("Vietcombank"), AccountNumber: new("0011223344"), AccountHolder: new("Gara Ánh Dương")},
	}, "tax_code,legal_name,legal_address,representative,bank")
	require.NoError(t, err)
}

// Scenario: E2E-BIL-007: the quote, the receipt and the payment voucher carry
// the garage's legal header, and the voucher writes the amount in words, in
// Vietnamese with its diacritics.
func TestDocumentsCarryTheGarageLegalHeader(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "bil-header")
	legalProfile(t, business)

	settlement := business.Settlement(t)

	paid, err := business.Owner.API.WithIdempotencyKey("bil-header-"+settlement.GetId()).RecordPayment(ctx, business.GarageID, settlement.GetId(),
		&billingv1.RecordSettlementPaymentRequest{Amount: fixture.LaborTotal, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
	require.NoError(t, err)
	require.Len(t, paid.GetPayments(), 1)

	_, receipt, err := business.Owner.API.DownloadReceiptPDF(ctx, business.GarageID, settlement.GetId())
	require.NoError(t, err)

	_, voucher, err := business.Owner.API.DownloadVoucherPDF(ctx, business.GarageID, settlement.GetId(), paid.GetPayments()[0].GetId())
	require.NoError(t, err)

	quote := business.DraftQuote(t)
	_, quotePDF, err := business.Owner.API.DownloadQuotePDF(ctx, business.GarageID, quote.GetId())
	require.NoError(t, err)

	header := []string{
		"Công ty TNHH Gara Ánh Dương", "12 Lê Lợi, Bến Nghé, Quận 1", "0312345601", "Nguyễn Văn Ánh", "Vietcombank",
	}

	for name, body := range map[string][]byte{"receipt": receipt, "voucher": voucher, "quote": quotePDF} {
		text := fixture.PDFText(t, body)

		for _, want := range header {
			assert.Contains(t, text, want, "the %s header", name)
		}
	}

	voucherText := fixture.PDFText(t, voucher)
	assert.Contains(t, voucherText, "Năm trăm nghìn đồng", "500,000 in words")
	assert.Contains(t, voucherText, "500,000", "and in figures")
	assert.Contains(t, voucherText, paid.GetPayments()[0].GetVoucherNumber(), "the voucher number")
	assert.Contains(t, voucherText, paid.GetPayments()[0].GetPayerName(), "the payer")
}

// Scenario: E2E-BIL-012: the receipt prints the billing party's legal name and
// tax ID, and keeps printing the ones it was issued with after the profile is
// edited.
func TestReceiptPrintsTheBillingPartySnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "bil-party-pdf")

	profile, err := business.Owner.API.CreateTaxProfile(ctx, business.GarageID, business.CustomerID,
		company("Công ty Hòa Bình", new("0312345678"), new("1 Nguyễn Huệ, Quận 1")))
	require.NoError(t, err)

	settlement := business.SettlementBilledTo(t, profile.GetId())

	_, before, err := business.Owner.API.DownloadReceiptPDF(ctx, business.GarageID, settlement.GetId())
	require.NoError(t, err)

	text := fixture.PDFText(t, before)
	assert.Contains(t, text, "Công ty Hòa Bình")
	assert.Contains(t, text, "0312345678")
	assert.Contains(t, text, "1 Nguyễn Huệ, Quận 1")

	_, err = business.Owner.API.UpdateTaxProfile(ctx, business.GarageID, business.CustomerID, profile.GetId(),
		&customerv1.CustomerTaxProfile{LegalName: "Công ty Hòa Bình Holdings", TaxId: new("0312345679")}, "legal_name,tax_id")
	require.NoError(t, err)

	_, after, err := business.Owner.API.DownloadReceiptPDF(ctx, business.GarageID, settlement.GetId())
	require.NoError(t, err)

	text = fixture.PDFText(t, after)
	assert.Contains(t, text, "Công ty Hòa Bình", "the receipt keeps the legal name it was issued with")
	assert.NotContains(t, text, "Holdings")
	assert.NotContains(t, text, "0312345679")
}
