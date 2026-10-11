// Package issuer holds the garage's legal identity as it was when a document
// was issued (FR-TEN-06). Changing the garage's profile never rewrites a
// document already issued: each document keeps its own copy, a snapshot
// ("bản chụp"), which is what its PDF prints.
package issuer

import "strings"

// Issuer is the legal identity printed in the header of a document: the
// legal name and address, tax code, representative, bank account, phone
// and the logo's object key. It is persisted as a JSON document next to
// the document it belongs to.
type Issuer struct {
	Name           string `json:"name"`
	Address        string `json:"address,omitempty"`
	Phone          string `json:"phone,omitempty"`
	TaxCode        string `json:"tax_code,omitempty"`
	Representative string `json:"representative,omitempty"`
	BankName       string `json:"bank_name,omitempty"`
	AccountNumber  string `json:"account_number,omitempty"`
	AccountHolder  string `json:"account_holder,omitempty"`
	// LogoKey is the object key of the logo at that time.
	LogoKey string `json:"logo_key,omitempty"`
}

// IsZero reports whether no identity was captured (a document that has no
// snapshot falls back to the garage's current profile).
func (i Issuer) IsZero() bool { return i.Name == "" }

// BankLine is the bank account on one line, "Vietcombank - 0011 - An Phat",
// empty when there is none.
func (i Issuer) BankLine() string {
	parts := make([]string, 0, 3)

	for _, part := range []string{i.BankName, i.AccountNumber, i.AccountHolder} {
		if part != "" {
			parts = append(parts, part)
		}
	}

	return strings.Join(parts, " - ")
}
