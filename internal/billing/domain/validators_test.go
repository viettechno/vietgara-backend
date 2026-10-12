package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

func TestSettlementStatus_IsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status domain.SettlementStatus
		want   bool
	}{
		{domain.StatusPending, true},
		{domain.StatusPartiallyPaid, true},
		{domain.StatusPaid, true},
		{"", false},
		{"settled", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, c.status.IsValid(), "status %q", c.status)
	}
}

func TestPaymentMethod_IsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method domain.PaymentMethod
		want   bool
	}{
		{domain.MethodCash, true},
		{domain.MethodBankTransfer, true},
		{domain.MethodOnline, true},
		{"", false},
		{"card", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, c.method.IsValid(), "method %q", c.method)
	}
}

func TestPayment_Voided(t *testing.T) {
	t.Parallel()

	assert.False(t, domain.Payment{}.Voided())

	now := time.Now()
	assert.True(t, domain.Payment{VoidedAt: &now}.Voided())
}
