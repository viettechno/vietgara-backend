package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

func TestPayable_DeriveRemainingAndAge(t *testing.T) {
	t.Parallel()

	p := &domain.Payable{TotalAmount: 700_000, PaidAmount: 200_000, ReceiptDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}

	p.Derive()
	require.Equal(t, domain.PayableOpen, p.Status)
	require.EqualValues(t, 500_000, p.Remaining())

	p.PaidAmount = 700_000
	p.Derive()
	require.Equal(t, domain.PayablePaid, p.Status)

	p.PaidAmount = 0
	p.Derive()
	require.Equal(t, domain.PayableOpen, p.Status, "a voided voucher reopens it")

	p.Status = domain.PayableCancelled
	p.PaidAmount = 700_000
	p.Derive()
	require.Equal(t, domain.PayableCancelled, p.Status, "a cancelled payable stays cancelled")

	require.Equal(t, 50, p.AgeDays(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)))
	require.Zero(t, p.AgeDays(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)), "a receipt dated tomorrow is not negative")
}

func TestPaymentOutVoucher_Amounts(t *testing.T) {
	t.Parallel()

	at := time.Now()
	v := &domain.PaymentOutVoucher{Amount: 1_000_000, Allocations: []domain.Allocation{
		{Amount: 600_000}, {Amount: 100_000, Cancelled: true}, {Amount: 200_000},
	}}

	require.EqualValues(t, 800_000, v.Allocated(), "an allocation to a cancelled payable does not count")
	require.EqualValues(t, 200_000, v.Unallocated())
	require.False(t, v.Voided())

	v.VoidedAt = &at
	require.True(t, v.Voided())
}
