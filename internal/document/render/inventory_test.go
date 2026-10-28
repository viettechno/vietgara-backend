package render_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
)

func TestGoodsReceiptPDF(t *testing.T) {
	t.Parallel()

	data := printmodel.GoodsReceipt{
		Garage: printmodel.GarageHeader{Name: "Gara Một"}, Number: "PN-202610-0001", Status: "completed", Date: "2026-10-15",
		DeliveryNoteNumber: "DN-9", Notes: "morning",
		Lines: []printmodel.GoodsReceiptRow{
			{SKU: "OIL", Name: "Dầu nhớt", Quantity: "10", Unit: "case", Supplier: "Phụ tùng Sài Gòn", UnitCost: 150000, Amount: 1500000},
		},
		TotalAmount: 1500000,
	}

	for _, showCost := range []bool{true, false} {
		data.ShowCost = showCost

		pdf, err := a4(data)
		require.NoError(t, err)
		assert.True(t, bytes.HasPrefix(pdf, []byte("%PDF")), "showCost=%v", showCost)
	}
}

func TestStockIssuePDF(t *testing.T) {
	t.Parallel()

	pdf, err := a4(printmodel.StockIssue{
		Garage: printmodel.GarageHeader{Name: "Gara Một"}, Number: "PX-202610-0007", Status: "posted", RepairOrderNumber: "LSC-1",
		PostedAt: "2026-10-15", PostedBy: "Lan",
		Lines: []printmodel.StockIssueRow{{SKU: "OIL", Name: "Dầu nhớt", Quantity: "4", Unit: "L"}, {SKU: "BOLT", Name: "Bu lông", Quantity: "2"}},
	})
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}

func TestPurchasesExcel(t *testing.T) {
	t.Parallel()

	data, err := render.PurchasesExcel([]render.PurchaseRow{
		{Date: "2026-10-10", Receipt: "PN-1", Supplier: "A", SKU: "OIL", Part: "Oil", Quantity: "240", Unit: "can", UnitCost: 260, Amount: 62500},
	}, 62500)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	rows, err := f.GetRows("Purchases")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "Amount (VND)", rows[0][8])
	assert.Equal(t, []string{"2026-10-10", "PN-1", "A", "OIL", "Oil", "240", "can", "260", "62500"}, rows[1])
	assert.Equal(t, "Total", rows[2][0])
	assert.Equal(t, "62500", rows[2][8])
}

func TestStockCountExcel(t *testing.T) {
	t.Parallel()

	counted := []render.StockCountRow{
		{SKU: "OIL", Part: "Oil", Unit: "can", System: "10", Counted: "12", Difference: "2", Result: "over", AverageCost: 50, Value: 100},
		{SKU: "FLT", Part: "Filter", Unit: "pc", System: "3", Result: "uncounted"},
	}

	read := func(data []byte) [][]string {
		t.Helper()

		f, err := excelize.OpenReader(bytes.NewReader(data))
		require.NoError(t, err)

		defer func() { _ = f.Close() }()

		rows, err := f.GetRows("Stock Count")
		require.NoError(t, err)

		return rows
	}

	withCost, err := render.StockCountExcel(counted, true, 100)
	require.NoError(t, err)

	rows := read(withCost)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"OIL", "Oil", "can", "10", "12", "2", "over", "50", "100"}, rows[1])
	assert.Equal(t, "Difference value (VND)", rows[0][8])
	assert.Equal(t, "100", rows[3][8])

	withoutCost, err := render.StockCountExcel(counted, false, 100)
	require.NoError(t, err)

	rows = read(withoutCost)
	assert.Equal(t, []string{"OIL", "Oil", "can", "10", "12", "2", "over"}, rows[1])
	assert.Len(t, rows[0], 7, "no cost columns without inventory.cost.read")
}

func TestStockValuationExcel(t *testing.T) {
	t.Parallel()

	data, err := render.StockValuationExcel([]render.ValuationRow{
		{Category: "Oils", SKU: "OIL", Part: "Oil", Quantity: "4", Unit: "can", AverageCost: 250, Value: 1000},
	}, 1000)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	rows, err := f.GetRows("Stock Valuation")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"Oils", "OIL", "Oil", "4", "can", "250", "1000"}, rows[1])
	assert.Equal(t, "1000", rows[2][6])
}
