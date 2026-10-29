package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
)

func TestType(t *testing.T) {
	t.Parallel()

	assert.Len(t, domain.AllTypes, 7)

	for _, typ := range domain.AllTypes {
		assert.True(t, typ.Valid(), typ)
	}

	assert.False(t, domain.Type("").Valid())
	assert.False(t, domain.Type("other").Valid())

	customer := []domain.Type{domain.TypeQuoteReady, domain.TypeVehicleReady, domain.TypePaymentReceived, domain.TypeSettlementReceipt}

	for _, typ := range domain.AllTypes {
		want := domain.AudienceStaff

		for _, c := range customer {
			if typ == c {
				want = domain.AudienceCustomer
			}
		}

		assert.Equal(t, want, typ.Audience(), typ)
	}

	for _, typ := range domain.AllTypes {
		assert.Equal(t, typ == domain.TypeQuoteReady || typ == domain.TypeSettlementReceipt, typ.Essential(), typ)
	}
}

func TestSettingsEnabled(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.Settings{}.Enabled(domain.TypeLowStock), "no settings means everything is on")
	assert.False(t, domain.Settings{Off: map[domain.Type]bool{domain.TypeLowStock: true}}.Enabled(domain.TypeLowStock))
}
