package render_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/document/render"
)

func TestStockMovementsExcel(t *testing.T) {
	t.Parallel()

	data, err := render.StockMovementsExcel([]render.StockMovementRow{{
		Date: "2026-10-01", SKU: "BEER-330", ItemName: "Beer", Direction: "In", Quantity: "240", BaseUnit: "can",
		EnteredQuantity: "10", EnteredUnit: "case", Reference: "Purchase",
	}})
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	rows, err := f.GetRows("Stock Movements")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"Date", "SKU", "Part", "Direction", "Quantity", "Unit", "Entered quantity", "Entered unit", "Reference"}, rows[0])
	assert.Equal(t, []string{"2026-10-01", "BEER-330", "Beer", "In", "240", "can", "10", "case", "Purchase"}, rows[1])
}

func TestRevenueExcel(t *testing.T) {
	t.Parallel()

	data, err := render.RevenueExcel([]render.RevenueRow{{Period: "2026-09", Amount: 1_000_000}, {Period: "2026-10", Amount: 500_000}}, 1_500_000)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	rows, err := f.GetRows("Revenue")
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"Period", "Revenue (VND)"}, rows[0])
	assert.Equal(t, []string{"Total", "1500000"}, rows[3])
}
