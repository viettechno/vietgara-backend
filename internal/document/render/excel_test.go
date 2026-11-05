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

func TestReceivablesExcel(t *testing.T) {
	t.Parallel()

	data, err := render.ReceivablesExcel([]render.ReceivableRow{
		{Number: "QT-1", Customer: "=HYPERLINK(\"http://x\")", Plate: "51A", DueDate: "2026-11-17", DaysOverdue: 3, Total: 1_000_000, Paid: 400_000, Remaining: 600_000, Status: "open"},
		{Number: "QT-2", Customer: "Bình", Plate: "51B", DueDate: "2026-11-30", Total: 500_000, Remaining: 500_000, Status: "open"},
	})
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	rows, err := f.GetRows("Receivables")
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, "Remaining (VND)", rows[0][7])
	assert.Equal(t, []string{"QT-1", "=HYPERLINK(\"http://x\")", "51A", "2026-11-17", "3", "1000000", "400000", "600000", "open"}, rows[1])
	assert.Equal(t, []string{"Total", "", "", "", "", "1500000", "400000", "1100000"}, rows[3][:8])

	formula, err := f.GetCellFormula("Receivables", "B2")
	require.NoError(t, err)
	assert.Empty(t, formula, "text that looks like a formula stays text")
}

func TestReconciliationExcel(t *testing.T) {
	t.Parallel()

	data, err := render.ReconciliationExcel(render.ReconciliationData{
		From: "2026-11-01", To: "2026-12-01", Opening: 400_000, Charged: 3_800_000, Collected: 1_800_000, Voided: 300_000, Closing: 2_700_000,
		PaidOut: 250_000, Matches: true,
		CollectedMethod: []render.MethodAmount{{Method: "bank_transfer", Amount: 500_000}, {Method: "cash", Amount: 1_300_000}},
		Lines:           []render.ReconciliationLineRow{{Date: "2026-11-02", Kind: "charge", Reference: "QT-1", Customer: "An", Amount: 1_000_000}},
	})
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	summary, err := f.GetRows("Summary")
	require.NoError(t, err)
	assert.Equal(t, []string{"Closing balance (VND)", "2700000"}, summary[5])
	assert.Equal(t, []string{"Ledger agrees with the receivables", "Yes"}, summary[7])
	assert.Equal(t, []string{"Collected by cash (VND)", "1300000"}, summary[10])

	lines, err := f.GetRows("Lines")
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"2026-11-02", "charge", "QT-1", "An", "", "1000000"}, lines[1])
	assert.Equal(t, "Day", lines[0][0])

	other, err := render.ReconciliationExcel(render.ReconciliationData{Difference: 5})
	require.NoError(t, err)

	g, err := excelize.OpenReader(bytes.NewReader(other))
	require.NoError(t, err)

	defer func() { _ = g.Close() }()

	rows, err := g.GetRows("Summary")
	require.NoError(t, err)
	assert.Equal(t, []string{"Ledger agrees with the receivables", "No"}, rows[7])
}
