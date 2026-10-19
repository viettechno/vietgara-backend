package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
)

func TestParseType(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]domain.Type{
		"parts": domain.TypeParts, "goods_receipts": domain.TypeGoodsReceipts, "stock_counts": domain.TypeStockCounts,
	} {
		got, ok := domain.ParseType(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "Parts", "receipts", "units"} {
		_, ok := domain.ParseType(bad)
		assert.False(t, ok, bad)
	}
}

func TestNormalizeHeader(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "base unit", domain.NormalizeHeader("  Base   UNIT "))
	assert.Equal(t, "sku", domain.NormalizeHeader("SKU"))
	assert.Empty(t, domain.NormalizeHeader("   "))
}
