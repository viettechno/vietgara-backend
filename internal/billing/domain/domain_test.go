package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

func TestSettlement_DeriveStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		total        int64
		customerPaid int64
		insurerPaid  int64
		want         domain.SettlementStatus
	}{
		{"nothing paid", 1_000_000, 0, 0, domain.StatusPending},
		{"partially paid by customer", 1_000_000, 400_000, 0, domain.StatusPartiallyPaid},
		{"partially paid by both", 1_000_000, 400_000, 100_000, domain.StatusPartiallyPaid},
		{"fully paid by customer alone", 1_000_000, 1_000_000, 0, domain.StatusPaid},
		{"fully paid across both payers", 1_000_000, 600_000, 400_000, domain.StatusPaid},
		{"zero-total settlement is immediately paid", 0, 0, 0, domain.StatusPaid},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			s := &domain.Settlement{TotalAmount: c.total, CustomerPaidAmount: c.customerPaid, InsurerPaidAmount: c.insurerPaid}
			s.DeriveStatus()
			require.Equal(t, c.want, s.Status)
		})
	}
}

func TestSettlement_RemainingDue(t *testing.T) {
	t.Parallel()

	s := &domain.Settlement{
		CustomerAmount: 700_000, InsurerAmount: 300_000,
		CustomerPaidAmount: 200_000, InsurerPaidAmount: 300_000,
	}

	require.Equal(t, int64(500_000), s.RemainingDue(domain.PayerCustomer))
	require.Equal(t, int64(0), s.RemainingDue(domain.PayerInsurer))
}

func TestValidateAllocation(t *testing.T) {
	t.Parallel()

	insurer := "insurer-1"

	require.NoError(t, domain.ValidateAllocation(1_000_000, 1_000_000, 0, nil))
	require.NoError(t, domain.ValidateAllocation(1_000_000, 600_000, 400_000, &insurer))

	require.ErrorIs(t, domain.ValidateAllocation(1_000_000, 600_000, 300_000, &insurer), domain.ErrInvalidAllocation)
	require.ErrorIs(t, domain.ValidateAllocation(1_000_000, -100, 1_100_000, &insurer), domain.ErrInvalidAllocation)
	require.ErrorIs(t, domain.ValidateAllocation(1_000_000, 600_000, 400_000, nil), domain.ErrInsurerPartnerRequired)
	require.ErrorIs(t, domain.ValidateAllocation(1_000_000, 600_000, 400_000, new("")), domain.ErrInsurerPartnerRequired)
}
