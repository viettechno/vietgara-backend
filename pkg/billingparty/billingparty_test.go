package billingparty_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

func TestParty(t *testing.T) {
	t.Parallel()

	assert.True(t, billingparty.Party{}.IsZero())
	assert.False(t, billingparty.Party{}.InvoiceReady(), "retail has no invoice")

	company := billingparty.Party{TaxProfileID: "p", LegalName: "An Phat", TaxID: "0301234567", Address: "12 Le Loi"}
	assert.False(t, company.IsZero())
	assert.True(t, company.InvoiceReady())

	for _, incomplete := range []billingparty.Party{
		{TaxProfileID: "p", TaxID: "0301234567", Address: "12 Le Loi"},
		{TaxProfileID: "p", LegalName: "An Phat", Address: "12 Le Loi"},
		{TaxProfileID: "p", LegalName: "An Phat", TaxID: "0301234567"},
	} {
		assert.False(t, incomplete.InvoiceReady())
	}
}
