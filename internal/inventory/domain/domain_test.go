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

func TestLineAmount(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(1_500_000), domain.LineAmount("10", 150000))
	assert.Equal(t, int64(2503), domain.LineAmount("2.5", 1001), "2502.5 rounds half up")
	assert.Equal(t, int64(0), domain.LineAmount("10", 0), "an unrecorded cost is zero")
	assert.Equal(t, int64(0), domain.LineAmount("many", 100))
}

func TestGoodsReceipt_Total(t *testing.T) {
	t.Parallel()

	r := domain.GoodsReceipt{Lines: []domain.GoodsReceiptLine{
		{EnteredQuantity: "10", UnitCost: 1000}, {EnteredQuantity: "0.5", UnitCost: 3}, {EnteredQuantity: "1"},
	}}

	assert.Equal(t, int64(10_002), r.Total(), "10000 + 1.5 rounded to 2 + 0")
}

func TestStockValueAndRounding(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1203", domain.StockValue("12", "100.25"))
	assert.Equal(t, "0.0003", domain.StockValue("0.001", "0.3"))
	assert.Equal(t, "0", domain.StockValue("x", "1"))

	n, ok := domain.WholeDong("100.5")
	require.True(t, ok)
	assert.Equal(t, int64(101), n)

	n, ok = domain.WholeDong("-100.5")
	require.True(t, ok)
	assert.Equal(t, int64(-101), n, "away from zero")

	_, ok = domain.WholeDong("abc")
	assert.False(t, ok)
}

func TestDecimalHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "3.5", domain.Add("2", "1.5"))
	assert.Equal(t, "2", domain.Add("", "2"), "empty counts as zero")
	assert.Equal(t, "0.5", domain.Subtract("2", "1.5"))
	assert.Equal(t, "-1", domain.Subtract("1", "2"))
	assert.Equal(t, "0", domain.Subtract("x", "1"))
	assert.True(t, domain.Less("9.999", "10"))
	assert.False(t, domain.Less("10", "10"))
	assert.True(t, domain.IsZero("0.000"))
	assert.True(t, domain.IsZero("not a number"))
	assert.False(t, domain.IsZero("0.001"))
}

func TestParseDate(t *testing.T) {
	t.Parallel()

	d, err := domain.ParseDate(" 2026-10-15 ")
	require.NoError(t, err)
	assert.Equal(t, 15, d.Day())

	for _, bad := range []string{"", "15/10/2026", "2026-13-01", "2026-10-15T10:00:00Z"} {
		_, err := domain.ParseDate(bad)
		require.ErrorIs(t, err, domain.ErrInvalidReceiptDate, bad)
	}
}

func TestValidatePartSuppliers(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidatePartSuppliers(nil))
	require.NoError(t, domain.ValidatePartSuppliers([]domain.PartSupplier{{SupplierID: "a", IsDefault: true}, {SupplierID: "b"}}))
	require.ErrorIs(t, domain.ValidatePartSuppliers([]domain.PartSupplier{{SupplierID: "a"}, {SupplierID: "a"}}), domain.ErrDuplicateSupplier)
	require.ErrorIs(t, domain.ValidatePartSuppliers([]domain.PartSupplier{{SupplierID: "a", IsDefault: true}, {SupplierID: "b", IsDefault: true}}),
		domain.ErrTooManyDefaults)
}

func TestGoodsReceiptStatus_IsValid(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.GoodsReceiptStatus{domain.GoodsReceiptDraft, domain.GoodsReceiptCompleted, domain.GoodsReceiptCancelled} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.GoodsReceiptStatus("").IsValid())
}
