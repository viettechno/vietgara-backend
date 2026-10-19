package main

import (
	"context"
	"strings"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	dataimportHandler "github.com/viettechno/vietgara-backend/internal/dataimport/handler"
	dataimportRepository "github.com/viettechno/vietgara-backend/internal/dataimport/repository"
	dataimportUsecase "github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	"github.com/viettechno/vietgara-backend/internal/dataimport/xlsx"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	supplierRepository "github.com/viettechno/vietgara-backend/internal/supplier/repository"
	supplierUsecase "github.com/viettechno/vietgara-backend/internal/supplier/usecase"
)

// dataImportModule is the wired Data Import module (FR-INV-14, Release 7.2).
type dataImportModule struct {
	server *dataimportHandler.Server
	uc     *dataimportUsecase.UseCase
	repo   *dataimportRepository.PostgresRepository
}

func buildDataImport(
	pool *platformdb.Pool, catalogue *catalogueUsecase.UseCase, inventory *inventoryUsecase.InventoryUseCase,
	suppliers *supplierRepository.PostgresRepository, audits dataimportUsecase.AuditLogger,
) *dataImportModule {
	repo := dataimportRepository.NewPostgresRepository(pool)
	uc := dataimportUsecase.NewUseCase(dataimportUsecase.Dependencies{
		Catalogue: dataImportCatalogue{uc: catalogue},
		Suppliers: dataImportSuppliers{repo: suppliers},
		Inventory: dataImportInventory{uc: inventory},
		Runs:      repo,
		Reader:    dataImportReader{},
		Audit:     audits,
	})

	return &dataImportModule{server: dataimportHandler.NewServer(uc), uc: uc, repo: repo}
}

// dataImportCatalogue gives Data Import the Catalogue use cases it needs to
// create parts, units, categories, packaging and barcodes exactly as the
// screens do.
type dataImportCatalogue struct {
	uc *catalogueUsecase.UseCase
}

func (c dataImportCatalogue) PartsBySKUs(ctx context.Context, garageID string, skus []string) ([]dataimportUsecase.PartInfo, error) {
	parts, err := c.uc.PartsBySKUs(ctx, garageID, skus)
	if err != nil {
		return nil, err
	}

	out := make([]dataimportUsecase.PartInfo, 0, len(parts))
	for _, p := range parts {
		out = append(out, importPartInfo(p))
	}

	return out, nil
}

func importPartInfo(p *catalogueDomain.Part) dataimportUsecase.PartInfo {
	info := dataimportUsecase.PartInfo{
		ID: p.ID, SKU: p.SKU, Name: p.Name, IsActive: p.IsActive, BaseUnitID: p.BaseUnitID,
		PackagingUnits: make([]dataimportUsecase.PackagingUnit, 0, len(p.PackagingUnits)),
	}

	for _, u := range p.PackagingUnits {
		info.PackagingUnits = append(info.PackagingUnits, dataimportUsecase.PackagingUnit{UnitID: u.UnitID, Factor: u.Factor})
	}

	return info
}

// PartsByBarcodes matches each scanned code to its part and the unit one
// scan stands for.
func (c dataImportCatalogue) PartsByBarcodes(
	ctx context.Context, garageID string, codes []string,
) (map[string]dataimportUsecase.BarcodeMatch, error) {
	parts, err := c.uc.PartsByBarcodes(ctx, garageID, codes)
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		wanted[strings.ToLower(code)] = struct{}{}
	}

	out := make(map[string]dataimportUsecase.BarcodeMatch, len(codes))

	for _, p := range parts {
		info := importPartInfo(p)

		for _, b := range p.Barcodes {
			key := strings.ToLower(b.Barcode)
			if _, ok := wanted[key]; !ok {
				continue
			}

			match := dataimportUsecase.BarcodeMatch{Part: info}
			if b.UnitID != nil {
				match.UnitID = *b.UnitID
			}

			out[key] = match
		}
	}

	return out, nil
}

func (c dataImportCatalogue) Units(ctx context.Context, garageID string) ([]dataimportUsecase.UnitInfo, error) {
	var out []dataimportUsecase.UnitInfo

	for page := 1; ; page++ {
		units, err := c.uc.ListUnits(ctx, catalogueUsecase.ListUnitsRequest{
			PageRequest: catalogueUsecase.PageRequest{Page: page, PageSize: catalogueUsecase.MaxPageSize},
			GarageID:    garageID, IncludeInactive: true,
		})
		if err != nil {
			return nil, err
		}

		for _, u := range units.Items {
			out = append(out, dataimportUsecase.UnitInfo{ID: u.ID, Name: u.Name, Abbreviation: u.Abbreviation, IsActive: u.IsActive})
		}

		if len(out) >= units.Total || len(units.Items) == 0 {
			break
		}
	}

	return out, nil
}

func (c dataImportCatalogue) Categories(ctx context.Context, garageID string) ([]dataimportUsecase.CategoryInfo, error) {
	categories, err := c.uc.ListCategories(ctx, garageID, true)
	if err != nil {
		return nil, err
	}

	out := make([]dataimportUsecase.CategoryInfo, 0, len(categories))
	for _, category := range categories {
		out = append(out, dataimportUsecase.CategoryInfo{ID: category.ID, Name: category.Name})
	}

	return out, nil
}

func (c dataImportCatalogue) CreateUnit(
	ctx context.Context, garageID, actorID, name, abbreviation string,
) (dataimportUsecase.UnitInfo, error) {
	unit, err := c.uc.CreateUnit(ctx, catalogueUsecase.CreateUnitRequest{
		GarageID: garageID, ActorID: actorID,
		Fields: catalogueUsecase.UnitFields{Name: &name, Abbreviation: &abbreviation},
	})
	if err != nil {
		return dataimportUsecase.UnitInfo{}, err
	}

	return dataimportUsecase.UnitInfo{ID: unit.ID, Name: unit.Name, Abbreviation: unit.Abbreviation, IsActive: unit.IsActive}, nil
}

func (c dataImportCatalogue) CreateCategory(
	ctx context.Context, garageID, actorID, name string,
) (dataimportUsecase.CategoryInfo, error) {
	category, err := c.uc.CreateCategory(ctx, catalogueUsecase.CreateCategoryRequest{
		GarageID: garageID, ActorID: actorID, Fields: catalogueUsecase.CategoryFields{Name: &name},
	})
	if err != nil {
		return dataimportUsecase.CategoryInfo{}, err
	}

	return dataimportUsecase.CategoryInfo{ID: category.ID, Name: category.Name}, nil
}

func (c dataImportCatalogue) CreateParts(
	ctx context.Context, garageID, actorID string, parts []dataimportUsecase.NewPart,
) ([]string, error) {
	in := make([]catalogueUsecase.NewPart, 0, len(parts))

	for _, p := range parts {
		packaging := make([]catalogueDomain.PackagingUnit, 0, len(p.Packaging))
		for _, u := range p.Packaging {
			packaging = append(packaging, catalogueDomain.PackagingUnit{UnitID: u.UnitID, Factor: u.Factor})
		}

		barcodes := make([]catalogueDomain.PartBarcode, 0, len(p.Barcodes))
		for _, code := range p.Barcodes {
			barcodes = append(barcodes, catalogueDomain.PartBarcode{Barcode: code.Barcode, UnitID: code.UnitID})
		}

		in = append(in, catalogueUsecase.NewPart{
			SKU: p.SKU, Name: p.Name, Brand: p.Brand, CategoryID: p.CategoryID, BaseUnitID: p.BaseUnitID,
			Packaging: packaging, Barcodes: barcodes,
		})
	}

	return c.uc.CreateParts(ctx, garageID, actorID, in)
}

func (c dataImportCatalogue) ExistingBarcodes(ctx context.Context, garageID string, codes []string) (map[string]string, error) {
	return c.uc.BarcodesByCode(ctx, garageID, codes)
}

// dataImportSuppliers resolves supplier names, which the import accepts but
// never creates (an unknown name is a row error).
type dataImportSuppliers struct {
	repo *supplierRepository.PostgresRepository
}

func (s dataImportSuppliers) IDsByName(ctx context.Context, garageID string, names []string) (map[string]string, error) {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	out := make(map[string]string, len(names))

	for offset := 0; ; offset += supplierUsecase.MaxPageSize {
		suppliers, total, err := s.repo.List(ctx, garageID, supplierUsecase.ListFilter{
			Limit: supplierUsecase.MaxPageSize, Offset: offset, IncludeInactive: true,
		})
		if err != nil {
			return nil, err
		}

		for _, supplier := range suppliers {
			if _, ok := wanted[strings.ToLower(supplier.Name)]; ok {
				out[supplier.Name] = supplier.ID
			}
		}

		if offset+len(suppliers) >= total || len(suppliers) == 0 {
			break
		}
	}

	return out, nil
}

// dataImportInventory writes imported opening stock, receipts and counts
// through the Inventory use cases.
type dataImportInventory struct {
	uc *inventoryUsecase.InventoryUseCase
}

func (i dataImportInventory) CreateGoodsReceipt(
	ctx context.Context, garageID, actorID string, canSetCost bool, in dataimportUsecase.ReceiptInput, complete bool,
) (dataimportUsecase.Document, error) {
	lines := make([]inventoryUsecase.ReceiptLineInput, 0, len(in.Lines))
	for _, line := range in.Lines {
		lines = append(lines, inventoryUsecase.ReceiptLineInput{
			PartID: line.PartID, UnitID: line.UnitID, EnteredQuantity: line.Quantity,
			SupplierID: line.SupplierID, UnitCost: line.UnitCost,
		})
	}

	receipt, err := i.uc.CreateGoodsReceipt(ctx, inventoryUsecase.CreateGoodsReceiptRequest{
		GarageID: garageID, ActorID: actorID, CanSetCost: canSetCost, ReceiptDate: in.ReceiptDate,
		DeliveryNoteNumber: in.DeliveryNote, Notes: in.Notes, Lines: lines,
	})
	if err != nil {
		return dataimportUsecase.Document{}, err
	}

	if complete {
		receipt, err = i.uc.CompleteGoodsReceipt(ctx, garageID, receipt.ID, actorID)
		if err != nil {
			return dataimportUsecase.Document{}, err
		}
	}

	return dataimportUsecase.Document{ID: receipt.ID, Number: receipt.Number}, nil
}

func (i dataImportInventory) CreateStockCount(
	ctx context.Context, garageID, actorID string, in dataimportUsecase.CountInput,
) (dataimportUsecase.Document, error) {
	lines := make([]inventoryUsecase.CountedPart, 0, len(in.Lines))
	for _, line := range in.Lines {
		lines = append(lines, inventoryUsecase.CountedPart{PartID: line.PartID, UnitID: line.UnitID, EnteredCounted: line.Quantity})
	}

	count, err := i.uc.ImportStockCount(ctx, inventoryUsecase.ImportStockCountRequest{
		GarageID: garageID, ActorID: actorID, Name: in.Name, Notes: in.Notes, Lines: lines,
	})
	if err != nil {
		return dataimportUsecase.Document{}, err
	}

	return dataimportUsecase.Document{ID: count.ID, Number: count.Number}, nil
}

// dataImportReader adapts the xlsx package to the use case's Reader port.
type dataImportReader struct{}

func (dataImportReader) Read(content []byte) (*dataimportUsecase.Sheet, error) {
	sheet, err := xlsx.Read(content)
	if err != nil {
		return nil, err
	}

	out := &dataimportUsecase.Sheet{Rows: make([]dataimportUsecase.SheetRow, 0, len(sheet.Rows))}
	for _, row := range sheet.Rows {
		out.Rows = append(out.Rows, dataimportUsecase.SheetRow{Number: row.Number, Cells: row.Cells})
	}

	return out, nil
}

// compile-time checks.
var (
	_ dataimportUsecase.Catalogue = dataImportCatalogue{}
	_ dataimportUsecase.Suppliers = dataImportSuppliers{}
	_ dataimportUsecase.Inventory = dataImportInventory{}
	_ dataimportUsecase.Reader    = dataImportReader{}
)
