// Package billingparty holds who a quote, repair order or settlement is
// billed to (FR-CUS-07) as it was when the document took it: a snapshot
// ("bản chụp") of the customer's tax profile, so a later edit of the
// profile never changes a document already issued. It is persisted as a
// JSON document next to the document it belongs to.
package billingparty

// Party is the legal identity a document is billed to. The zero Party is
// the retail case: the customer is billed under their own name.
type Party struct {
	// TaxProfileID is the profile the snapshot was taken from.
	TaxProfileID string `json:"tax_profile_id,omitempty"`
	// Kind is "personal" or "company".
	Kind         string `json:"kind,omitempty"`
	LegalName    string `json:"legal_name,omitempty"`
	TaxID        string `json:"tax_id,omitempty"`
	Address      string `json:"address,omitempty"`
	InvoiceEmail string `json:"invoice_email,omitempty"`
}

// IsZero reports whether no profile was chosen (a retail receipt).
func (p Party) IsZero() bool { return p.TaxProfileID == "" }

// InvoiceReady reports whether a VAT invoice can be issued to the party: a
// legal name, a tax id and an address are on file.
func (p Party) InvoiceReady() bool {
	return !p.IsZero() && p.LegalName != "" && p.TaxID != "" && p.Address != ""
}
