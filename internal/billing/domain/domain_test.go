package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

func TestSettlement_DeriveStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		total int64
		paid  int64
		want  domain.SettlementStatus
	}{
		{"nothing paid", 1_000_000, 0, domain.StatusPending},
		{"partially paid", 1_000_000, 400_000, domain.StatusPartiallyPaid},
		{"fully paid", 1_000_000, 1_000_000, domain.StatusPaid},
		{"zero-total settlement is immediately paid", 0, 0, domain.StatusPaid},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			s := &domain.Settlement{TotalAmount: c.total, PaidAmount: c.paid}
			s.DeriveStatus()
			require.Equal(t, c.want, s.Status)
		})
	}
}

func TestSettlement_RemainingDue(t *testing.T) {
	t.Parallel()

	s := &domain.Settlement{TotalAmount: 1_000_000, PaidAmount: 200_000}

	require.Equal(t, int64(800_000), s.RemainingDue())
}
