package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	receivableDomain "github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

// The Receivables module copies the placeholder of the Customer module so that
// it imports nothing of it; nothing but this test ties the two together.
func TestAnonymizedPlaceholdersAreInStep(t *testing.T) {
	t.Parallel()

	assert.Equal(t, customerDomain.AnonymizedName, receivableDomain.AnonymizedName)
	assert.Equal(t, receivableDomain.AnonymizedPlate, customerDomain.AnonymizedPlatePrefix[:len(receivableDomain.AnonymizedPlate)])
}
