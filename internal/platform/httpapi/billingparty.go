package httpapi

import (
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

// BillingPartyToProto maps the snapshot a document keeps of who it is billed
// to (FR-CUS-07) to its wire form, for every handler that returns one. A
// retail receipt has no party, which is nil on the wire.
func BillingPartyToProto(p billingparty.Party) *commonv1.BillingParty {
	if p.IsZero() {
		return nil
	}

	kind := commonv1.TaxProfileKind_TAX_PROFILE_KIND_UNSPECIFIED

	switch p.Kind {
	case "personal":
		kind = commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL
	case "company":
		kind = commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY
	}

	return &commonv1.BillingParty{
		TaxProfileId: p.TaxProfileID, Kind: kind, LegalName: p.LegalName, TaxId: p.TaxID, Address: p.Address,
		InvoiceEmail: p.InvoiceEmail,
	}
}

// BillingTaxProfileID reads the profile a client chose from the billing
// party it sent: nil when it sent none (the customer's default applies), ""
// when it chose a retail receipt explicitly.
func BillingTaxProfileID(p *commonv1.BillingParty) *string {
	if p == nil {
		return nil
	}

	id := p.GetTaxProfileId()

	return &id
}
