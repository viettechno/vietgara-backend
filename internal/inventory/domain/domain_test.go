package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

func TestIsLow(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.IsLow("2", "5"))
	assert.True(t, domain.IsLow("5", "5"))
	assert.True(t, domain.IsLow("-1", "0"), "negative stock is low")
	assert.False(t, domain.IsLow("5.001", "5"))
	assert.False(t, domain.IsLow("not-a-number", "5"))
	assert.False(t, domain.IsLow("5", "not-a-number"))
}

func TestParseQuantity(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"1", "0.001", "1.5", " 240 ", "9999999999"} {
		got, err := domain.ParseQuantity(ok)
		require.NoError(t, err, ok)
		assert.NotEmpty(t, got)
	}

	for _, bad := range []string{"", "0", "0.000", "-1", "1.2345", "abc", "1e3", "12345678901"} {
		_, err := domain.ParseQuantity(bad)
		require.ErrorIs(t, err, domain.ErrInvalidQuantity, bad)
	}
}

func TestCostPerBaseUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cost          int64
		entered, base string
		want          string
	}{
		{"base unit", 5_000, "10", "10", "5000"},
		{"per case of 24", 120_000, "10", "240", "5000"},
		{"keeps four decimals", 100_000, "1", "3", "33333.3333"},
		{"rounds half up", 1, "1", "8", "0.125"},
		{"fractional entered", 100, "0.5", "12", "4.1667"},
	}

	for _, tt := range tests {
		got, err := domain.CostPerBaseUnit(tt.cost, tt.entered, tt.base)
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, got, tt.name)
	}

	_, err := domain.CostPerBaseUnit(-1, "1", "1")
	require.ErrorIs(t, err, domain.ErrInvalidCost)

	_, err = domain.CostPerBaseUnit(1, "x", "1")
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)

	_, err = domain.CostPerBaseUnit(1, "1", "0")
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)
}
