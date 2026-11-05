package render

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// ReceivableRow is one receivable of the receivables workbook (FR-DOC-02).
type ReceivableRow struct {
	Number      string
	Customer    string
	Plate       string
	DueDate     string
	DaysOverdue int
	Total       int64
	Paid        int64
	Remaining   int64
	Status      string
}

const receivablesSheet = "Receivables"

// ReceivablesExcel renders the receivables list as an Excel workbook: one row
// per receivable and a total row. Amounts are numbers, so the sheet can be
// summed.
func ReceivablesExcel(rows []ReceivableRow) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", receivablesSheet); err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	headers := []any{"Settlement", "Customer", "Plate", "Due date", "Days overdue", "Total (VND)", "Paid (VND)", "Remaining (VND)", "Status"}
	if err := f.SetSheetRow(receivablesSheet, "A1", &headers); err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	if err := f.SetRowStyle(receivablesSheet, 1, 1, bold); err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	var total, paid, remaining int64

	for i, r := range rows {
		cells := []any{r.Number, r.Customer, r.Plate, r.DueDate, r.DaysOverdue, r.Total, r.Paid, r.Remaining, r.Status}
		if err := f.SetSheetRow(receivablesSheet, fmt.Sprintf("A%d", i+2), &cells); err != nil {
			return nil, fmt.Errorf("render receivables excel: %w", err)
		}

		total, paid, remaining = total+r.Total, paid+r.Paid, remaining+r.Remaining
	}

	totalRow := len(rows) + 2
	cells := []any{labelTotal, "", "", "", "", total, paid, remaining, ""}

	if err := f.SetSheetRow(receivablesSheet, fmt.Sprintf("A%d", totalRow), &cells); err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	if err := f.SetRowStyle(receivablesSheet, totalRow, totalRow, bold); err != nil {
		return nil, fmt.Errorf("render receivables excel: %w", err)
	}

	for col, width := range map[string]float64{"A": 16, "B": 28, "C": 14, "D": 12, "E": 13, "F": 16, "G": 16, "H": 16, "I": 10} {
		if err := f.SetColWidth(receivablesSheet, col, col, width); err != nil {
			return nil, fmt.Errorf("render receivables excel: %w", err)
		}
	}

	return write(f, "receivables")
}

// ReconciliationLineRow is one movement of the reconciliation workbook.
type ReconciliationLineRow struct {
	Date      string
	Kind      string
	Reference string
	Customer  string
	Method    string
	Amount    int64
}

// MethodAmount is the amount collected by one payment method.
type MethodAmount struct {
	Method string
	Amount int64
}

// ReconciliationData is the reconciliation of a period as the workbook shows
// it (FR-REC-04).
type ReconciliationData struct {
	From, To        string
	Opening         int64
	Charged         int64
	Collected       int64
	Voided          int64
	Closing         int64
	PaidOut         int64
	Matches         bool
	Difference      int64
	CollectedMethod []MethodAmount
	Lines           []ReconciliationLineRow
}

const (
	headingDay                 = "Day"
	reconciliationSummarySheet = "Summary"
	reconciliationLinesSheet   = "Lines"
)

// ReconciliationExcel renders the reconciliation of a period as an Excel
// workbook: a sheet of the totals and a sheet of the movements.
func ReconciliationExcel(r ReconciliationData) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", reconciliationSummarySheet); err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	if _, err := f.NewSheet(reconciliationLinesSheet); err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	match := "Yes"
	if !r.Matches {
		match = "No"
	}

	summary := [][]any{
		{"Period", r.From + " to " + r.To + " (end exclusive)"},
		{"Opening balance (VND)", r.Opening},
		{"Charged (VND)", r.Charged},
		{"Collected (VND)", r.Collected},
		{"Voided (VND)", r.Voided},
		{"Closing balance (VND)", r.Closing},
		{"Paid out to suppliers (VND)", r.PaidOut},
		{"Ledger agrees with the receivables", match},
		{"Difference (VND)", r.Difference},
	}

	for _, m := range r.CollectedMethod {
		summary = append(summary, []any{"Collected by " + m.Method + " (VND)", m.Amount})
	}

	for i, row := range summary {
		if err := f.SetSheetRow(reconciliationSummarySheet, fmt.Sprintf("A%d", i+1), &row); err != nil {
			return nil, fmt.Errorf("render reconciliation excel: %w", err)
		}
	}

	if err := f.SetColWidth(reconciliationSummarySheet, "A", "A", 38); err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	headers := []any{headingDay, "Kind", "Reference", "Customer", "Method", "Amount (VND)"}
	if err := f.SetSheetRow(reconciliationLinesSheet, "A1", &headers); err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	if err := f.SetRowStyle(reconciliationLinesSheet, 1, 1, bold); err != nil {
		return nil, fmt.Errorf("render reconciliation excel: %w", err)
	}

	for i, l := range r.Lines {
		cells := []any{l.Date, l.Kind, l.Reference, l.Customer, l.Method, l.Amount}
		if err := f.SetSheetRow(reconciliationLinesSheet, fmt.Sprintf("A%d", i+2), &cells); err != nil {
			return nil, fmt.Errorf("render reconciliation excel: %w", err)
		}
	}

	return write(f, "reconciliation")
}

func write(f *excelize.File, what string) ([]byte, error) {
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("render %s excel: %w", what, err)
	}

	return buf.Bytes(), nil
}
