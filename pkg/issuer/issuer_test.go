package issuer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

func TestIssuer(t *testing.T) {
	t.Parallel()

	assert.True(t, issuer.Issuer{}.IsZero())
	assert.False(t, issuer.Issuer{Name: "Gara"}.IsZero())

	assert.Empty(t, issuer.Issuer{}.BankLine())
	assert.Equal(t, "VCB", issuer.Issuer{BankName: "VCB"}.BankLine())
	assert.Equal(t, "VCB - 001 - An Phat", issuer.Issuer{BankName: "VCB", AccountNumber: "001", AccountHolder: "An Phat"}.BankLine())
	assert.Equal(t, "001", issuer.Issuer{AccountNumber: "001"}.BankLine())
}
