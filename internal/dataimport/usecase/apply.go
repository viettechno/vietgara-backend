package usecase

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
)

// errBarcodeUnit means a barcode names a packaging unit the row does not
// have; the preview refuses such a row, so it is a drift between the check
// and the write.
var errBarcodeUnit = errors.New("a barcode names a packaging unit the part does not have")

// apply writes a checked file through the Catalogue and Inventory use cases.
// It runs in the caller's garage-scoped transaction, so any error rolls the
// whole import back.
func (uc *UseCase) apply(ctx context.Context, req Request, checked *checkedFile) (*domain.Result, string, error) {
	switch req.Type {
	case domain.TypeParts:
		return uc.applyParts(ctx, req, checked)
	case domain.TypeGoodsReceipts:
		return uc.applyReceipts(ctx, req, checked)
	case domain.TypeStockCounts:
		return uc.applyCounts(ctx, req, checked)
	default:
		return nil, "", domain.ErrUnsupportedType
	}
}

func (uc *UseCase) applyParts(ctx context.Context, req Request, checked *checkedFile) (*domain.Result, string, error) {
	units, err := uc.catalogue.Units(ctx, req.GarageID)
	if err != nil {
		return nil, "", err
	}

	unitByName := make(map[string]UnitInfo, len(units))
	for _, u := range units {
		unitByName[strings.ToLower(u.Name)] = u
	}

	categories, err := uc.catalogue.Categories(ctx, req.GarageID)
	if err != nil {
		return nil, "", err
	}

	categoryByName := make(map[string]CategoryInfo, len(categories))
	for _, c := range categories {
		categoryByName[strings.ToLower(c.Name)] = c
	}

	parts := make([]NewPart, 0, len(checked.parts))

	for _, row := range checked.parts {
		part, err := uc.newPart(ctx, req, row, unitByName, categoryByName)
		if err != nil {
			return nil, "", err
		}

		parts = append(parts, part)
	}

	partIDs, err := uc.catalogue.CreateParts(ctx, req.GarageID, req.ActorID, parts)
	if err != nil {
		return nil, "", err
	}

	lines := make([]ReceiptLine, 0, len(checked.parts))

	for i, row := range checked.parts {
		if row.openingQty != "" && row.openingQty != "0" {
			lines = append(lines, ReceiptLine{
				PartID: partIDs[i], Quantity: row.openingQty, UnitCost: row.unitCost, SupplierID: row.supplierID,
			})
		}
	}

	result := &domain.Result{
		Type: req.Type, RowsImported: len(checked.parts), Counts: copyCounts(checked.preview.Counts),
	}

	if len(lines) == 0 {
		return result, "", nil
	}

	document, err := uc.inventory.CreateGoodsReceipt(ctx, req.GarageID, req.ActorID, req.Permissions.InventoryCostRead, ReceiptInput{
		Notes: importNote(req.FileName), Lines: lines,
	}, true)
	if err != nil {
		return nil, "", err
	}

	result.DocumentID, result.DocumentNumber = document.ID, document.Number

	return result, document.ID, nil
}

// newPart turns a checked row into the part to create, creating the units
// and the category the row names when they do not exist yet.
func (uc *UseCase) newPart(
	ctx context.Context, req Request, row partRow, unitByName map[string]UnitInfo, categoryByName map[string]CategoryInfo,
) (NewPart, error) {
	base, err := uc.ensureUnit(ctx, req, unitByName, row.baseUnit, row.baseUnitAbbr)
	if err != nil {
		return NewPart{}, err
	}

	part := NewPart{SKU: row.sku, Name: row.name, Brand: row.brand, BaseUnitID: base.ID}

	if row.category != "" {
		category, err := uc.ensureCategory(ctx, req, categoryByName, row.category)
		if err != nil {
			return NewPart{}, err
		}

		part.CategoryID = category.ID
	}

	packIDs := make(map[string]string, len(row.packaging))

	for _, pack := range row.packaging {
		unit, err := uc.ensureUnit(ctx, req, unitByName, pack.name, "")
		if err != nil {
			return NewPart{}, err
		}

		part.Packaging = append(part.Packaging, PackagingUnit{UnitID: unit.ID, Factor: pack.factor})
		packIDs[strings.ToLower(pack.name)] = unit.ID
	}

	for _, code := range row.barcodes {
		in := BarcodeInput{Barcode: code.code}

		if code.unit != "" {
			unitID, ok := packIDs[strings.ToLower(code.unit)]
			if !ok {
				return NewPart{}, fmt.Errorf("%w: %s", errBarcodeUnit, code.unit)
			}

			in.UnitID = &unitID
		}

		part.Barcodes = append(part.Barcodes, in)
	}

	return part, nil
}

func (uc *UseCase) applyReceipts(ctx context.Context, req Request, checked *checkedFile) (*domain.Result, string, error) {
	if len(checked.receipts) == 0 {
		return &domain.Result{Type: req.Type, Counts: map[string]int{"lines": 0}}, "", nil
	}

	lines := make([]ReceiptLine, 0, len(checked.receipts))

	for _, row := range checked.receipts {
		lines = append(lines, ReceiptLine{
			PartID: row.partID, UnitID: row.unitID, Quantity: row.quantity, UnitCost: row.unitCost, SupplierID: row.supplierID,
		})
	}

	document, err := uc.inventory.CreateGoodsReceipt(ctx, req.GarageID, req.ActorID, req.Permissions.InventoryCostRead, ReceiptInput{
		ReceiptDate: checked.receiptDate, DeliveryNote: checked.deliveryNote, Notes: checked.receiptNotes,
		Lines: lines,
	}, false)
	if err != nil {
		return nil, "", err
	}

	result := &domain.Result{
		Type: req.Type, RowsImported: len(checked.receipts), Counts: copyCounts(checked.preview.Counts),
		DocumentID: document.ID, DocumentNumber: document.Number,
	}

	return result, document.ID, nil
}

func (uc *UseCase) applyCounts(ctx context.Context, req Request, checked *checkedFile) (*domain.Result, string, error) {
	if len(checked.counts) == 0 {
		return &domain.Result{Type: req.Type, Counts: map[string]int{"lines": 0}}, "", nil
	}

	name := checked.countName
	if strings.TrimSpace(name) == "" {
		name = "Excel import"
	}

	lines := make([]CountLine, 0, len(checked.counts))

	for _, row := range checked.counts {
		lines = append(lines, CountLine{PartID: row.partID, UnitID: row.unitID, Quantity: row.quantity})
	}

	document, err := uc.inventory.CreateStockCount(ctx, req.GarageID, req.ActorID, CountInput{
		Name: name, Notes: checked.countNotes, Lines: lines,
	})
	if err != nil {
		return nil, "", err
	}

	result := &domain.Result{
		Type: req.Type, RowsImported: len(checked.counts), Counts: copyCounts(checked.preview.Counts),
		DocumentID: document.ID, DocumentNumber: document.Number,
	}

	return result, document.ID, nil
}

// ensureUnit returns the unit named name, creating it with abbreviation when
// it does not exist. An empty abbreviation means the name itself.
func (uc *UseCase) ensureUnit(
	ctx context.Context, req Request, byName map[string]UnitInfo, name, abbreviation string,
) (UnitInfo, error) {
	if unit, ok := byName[strings.ToLower(name)]; ok {
		return unit, nil
	}

	if abbreviation == "" {
		abbreviation = name
	}

	unit, err := uc.catalogue.CreateUnit(ctx, req.GarageID, req.ActorID, name, abbreviation)
	if err != nil {
		return UnitInfo{}, err
	}

	byName[strings.ToLower(name)] = unit

	return unit, nil
}

func (uc *UseCase) ensureCategory(
	ctx context.Context, req Request, byName map[string]CategoryInfo, name string,
) (CategoryInfo, error) {
	if category, ok := byName[strings.ToLower(name)]; ok {
		return category, nil
	}

	category, err := uc.catalogue.CreateCategory(ctx, req.GarageID, req.ActorID, name)
	if err != nil {
		return CategoryInfo{}, err
	}

	byName[strings.ToLower(name)] = category

	return category, nil
}

func copyCounts(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	maps.Copy(out, in)

	return out
}

func importNote(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return "Imported from Excel"
	}

	return "Imported from " + fileName
}
