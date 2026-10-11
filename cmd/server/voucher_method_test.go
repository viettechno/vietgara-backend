package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	billingDomain "github.com/viettechno/vietgara-backend/internal/billing/domain"
)

func TestVoucherMethod(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Tiền mặt", voucherMethod(billingDomain.MethodCash))
	assert.Equal(t, "Chuyển khoản", voucherMethod(billingDomain.MethodBankTransfer))
	assert.Equal(t, "Thanh toán trực tuyến", voucherMethod(billingDomain.MethodOnline))
	assert.Equal(t, "other", voucherMethod("other"))
}
