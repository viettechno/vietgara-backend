package usecase

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// PurchasesReport is the purchases-by-supplier report (FR-INV-19).
type PurchasesReport struct {
	Lines       []domain.PurchaseLine
	TotalAmount int64
}

// PurchasesReportRequest is the input for InventoryUseCase.PurchasesReport.
type PurchasesReportRequest struct {
	GarageID   string
	SupplierID string
	// From is inclusive and To exclusive, "YYYY-MM-DD"; empty is unbounded.
	From string
	To   string
}

// PurchasesReport lists the lines of the garage's completed goods receipts
// by supplier and period (FR-INV-19), with the total over the range.
func (uc *InventoryUseCase) PurchasesReport(ctx context.Context, req PurchasesReportRequest) (*PurchasesReport, error) {
	const op = "purchases report"

	if err := checkRange(req.From, req.To); err != nil {
		return nil, toAppError(op, err)
	}

	lines, err := uc.repo.PurchasesReport(ctx, req.GarageID, PurchaseFilter{SupplierID: req.SupplierID, From: req.From, To: req.To})
	if err != nil {
		return nil, toAppError(op, err)
	}

	partIDs, supplierIDList := make([]string, 0, len(lines)), make([]string, 0, len(lines))
	for _, l := range lines {
		partIDs = append(partIDs, l.PartID)
		supplierIDList = append(supplierIDList, l.SupplierID)
	}

	parts, err := uc.partsByID(ctx, req.GarageID, partIDs)
	if err != nil {
		return nil, toAppError(op, err)
	}

	names, err := uc.supplierNames(ctx, req.GarageID, supplierIDList)
	if err != nil {
		return nil, toAppError(op, err)
	}

	report := &PurchasesReport{Lines: lines}

	for i := range report.Lines {
		l := &report.Lines[i]
		p := parts[l.PartID]
		l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation
		l.SupplierName = names[l.SupplierID]
		report.TotalAmount += l.Amount
	}

	return report, nil
}

func checkRange(from, to string) error {
	var start, end time.Time

	if from != "" {
		t, err := time.Parse(time.DateOnly, from)
		if err != nil {
			return domain.ErrInvalidReport
		}

		start = t
	}

	if to != "" {
		t, err := time.Parse(time.DateOnly, to)
		if err != nil {
			return domain.ErrInvalidReport
		}

		end = t
	}

	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return domain.ErrInvalidReport
	}

	return nil
}

// StockValuation is the stock valuation report (FR-INV-07).
type StockValuation struct {
	Lines      []domain.StockValuationLine
	Categories []domain.StockValuationCategory
	TotalValue int64
	// At is the date valued at, "YYYY-MM-DD".
	At string
}

// StockValuation values every part at a date ("YYYY-MM-DD", empty for
// today in the garage's time zone): the quantity and weighted-average cost
// after the part's last movement up to the end of that day, per part and per
// category (FR-INV-07). Parts without movements are not listed.
func (uc *InventoryUseCase) StockValuation(ctx context.Context, garageID, at string) (*StockValuation, error) {
	const op = "stock valuation"

	garage, err := uc.garages.Get(ctx, garageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	day := uc.localNow(garage.Timezone)

	if at != "" {
		parsed, err := time.Parse(time.DateOnly, at)
		if err != nil {
			return nil, toAppError(op, domain.ErrInvalidReport)
		}

		day = parsed
	}

	loc := day.Location()
	if tz, err := time.LoadLocation(garage.Timezone); err == nil {
		loc = tz
	}

	y, m, d := day.Date()
	upTo := time.Date(y, m, d+1, 0, 0, 0, 0, loc)

	rows, err := uc.repo.StockValuation(ctx, garageID, upTo)
	if err != nil {
		return nil, toAppError(op, err)
	}

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.PartID)
	}

	parts, err := uc.partsByID(ctx, garageID, ids)
	if err != nil {
		return nil, toAppError(op, err)
	}

	return buildValuation(rows, parts, time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)), nil
}

func buildValuation(rows []ValuationRow, parts map[string]PartInfo, at string) *StockValuation {
	report := &StockValuation{At: at}
	byCategory := map[string]*domain.StockValuationCategory{}

	for _, r := range rows {
		if domain.IsZero(r.Quantity) {
			continue
		}

		p := parts[r.PartID]
		avg, _ := domain.WholeDong(r.AverageCost)
		value, _ := domain.WholeDong(domain.StockValue(r.Quantity, r.AverageCost))

		report.Lines = append(report.Lines, domain.StockValuationLine{
			PartID: r.PartID, PartSKU: p.SKU, PartName: p.Name, CategoryID: p.CategoryID, CategoryName: p.CategoryName,
			QuantityOnHand: r.Quantity, BaseUnitAbbreviation: p.BaseUnitAbbreviation, AverageCost: avg, Value: value,
		})

		c := byCategory[p.CategoryID]
		if c == nil {
			c = &domain.StockValuationCategory{CategoryID: p.CategoryID, CategoryName: p.CategoryName}
			byCategory[p.CategoryID] = c
		}

		c.Value += value
		report.TotalValue += value
	}

	slices.SortFunc(report.Lines, func(a, b domain.StockValuationLine) int {
		return cmp.Or(cmp.Compare(a.CategoryName, b.CategoryName), cmp.Compare(a.PartName, b.PartName), cmp.Compare(a.PartID, b.PartID))
	})

	for _, c := range byCategory {
		report.Categories = append(report.Categories, *c)
	}

	slices.SortFunc(report.Categories, func(a, b domain.StockValuationCategory) int {
		return cmp.Or(cmp.Compare(a.CategoryName, b.CategoryName), cmp.Compare(a.CategoryID, b.CategoryID))
	})

	return report
}
