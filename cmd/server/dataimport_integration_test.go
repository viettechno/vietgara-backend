//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	importUsecase "github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	supplierRepository "github.com/viettechno/vietgara-backend/internal/supplier/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// importWorkbook renders rows (header name to text) into an .xlsx with the
// template's columns; every cell is text, as the template's code columns are.
func importWorkbook(t *testing.T, typ domain.Type, rows []map[string]string) []byte {
	t.Helper()

	f := excelize.NewFile()

	defer func() { _ = f.Close() }()

	headers := template.Columns(typ)
	require.NoError(t, f.SetSheetRow("Sheet1", "A1", &headers))

	for i, row := range rows {
		cells := make([]any, len(headers))
		for c, header := range headers {
			cells[c] = row[header]
		}

		cell, err := excelize.CoordinatesToCellName(1, i+2)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow("Sheet1", cell, &cells))
	}

	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	return buf.Bytes()
}

// partsFile builds n part rows. Every tenth part has a Case packaging unit
// with its own barcode, and every part but each seventh has opening stock.
func partsFile(n int, prefix string) []map[string]string {
	rows := make([]map[string]string, 0, n)

	for i := range n {
		row := map[string]string{
			template.ColSKU: fmt.Sprintf("%s-%04d", prefix, i), template.ColName: fmt.Sprintf("Part %d", i),
			template.ColBaseUnit: "Piece", template.ColBaseUnitAbbr: "pc", template.ColCategory: "Filters",
			template.ColBrand: "Bosch", template.ColBarcodes: fmt.Sprintf("89%s%06d", prefix, i),
		}

		if i%10 == 0 {
			row[template.ColPackagingUnits] = "Case=12"
			row[template.ColBarcodes] += fmt.Sprintf(";79%s%06d@Case", prefix, i)
		}

		if i%7 != 0 {
			row[template.ColOpeningQuantity] = strconv.Itoa(i%5 + 1)
			row[template.ColUnitCost] = strconv.Itoa(1000 + i)
			row[template.ColSupplier] = "ACME"
		}

		rows = append(rows, row)
	}

	return rows
}

func reasonOfError(err error) string {
	if appErr, ok := apperror.As(err); ok {
		return appErr.Reason
	}

	return ""
}

// TestDataImport runs the Excel import (FR-INV-14) and barcodes (FR-INV-13)
// against a real PostgreSQL through the wiring of cmd/server: the 500-row
// acceptance file with 5 bad rows, all-or-nothing, idempotency, packaging
// barcodes, receipts and counts by barcode, garage isolation and the limits.
//
//nolint:funlen // one scenario through the real schema, read top to bottom
func TestDataImport(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	count := func(query string, args ...any) int {
		t.Helper()

		var n int

		require.NoError(t, pg.Admin.QueryRowContext(t.Context(), query, args...).Scan(&n))

		return n
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)
	exec(`INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $2, 'ACME'), ($3, $4, 'ACME')`,
		uuid.NewString(), garageID, uuid.NewString(), otherGarageID)

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	entities := &fileEntities{}
	files := buildFiles(pool, store, tenantRepo.NewPostgresGarageRepository(pool), catalogueRepository.NewPostgresRepository(pool), entities)
	usage := &catalogueUsage{}
	catalogue := buildCatalogue(pool, files, usage)
	entities.parts = catalogue.uc
	usage.inventory = nil

	quotes := quoteRepository.NewPostgresRepository(pool)
	usage.quotes, usage.repairOrders = quotes, repairorderRepository.NewPostgresRepository(pool)

	supplierRepo := supplierRepository.NewPostgresRepository(pool)
	inv := buildInventory(pool, outbox.NewEventPublisher(pool), inventoryParts{partQuantities{uc: catalogue.uc}},
		inventoryGarages{garages: tenantRepo.NewPostgresGarageRepository(pool)},
		inventoryDirectory{suppliers: supplierRepo, users: identityRepo.NewPostgresUserRepository(pool)},
		audit.NewLogger(pool))
	usage.inventory = inv.repo

	imports := buildDataImport(pool, catalogue.uc, inv.uc, supplierRepo, audit.NewLogger(pool)).uc

	inGarage := func(id string, fn func(ctx context.Context)) {
		t.Helper()

		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: id, UserID: userID}, func(ctx context.Context) error {
			fn(access.WithAccess(ctx, &access.Access{GarageID: id, UserID: userID, OwnerUserID: userID, Owner: true, Active: true}))

			return nil
		}))
	}

	all := importUsecase.Permissions{CatalogueWrite: true, InventoryWrite: true, InventoryCostRead: true, InventoryCount: true}

	request := func(garage string, typ domain.Type, content []byte, perms importUsecase.Permissions) importUsecase.Request {
		return importUsecase.Request{
			GarageID: garage, ActorID: userID, Type: typ, FileName: string(typ) + ".xlsx", Content: content, Permissions: perms,
		}
	}

	preview := func(garage string, typ domain.Type, content []byte) *domain.Preview {
		t.Helper()

		var p *domain.Preview

		inGarage(garage, func(ctx context.Context) {
			var err error

			p, err = imports.Preview(ctx, request(garage, typ, content, all))
			require.NoError(t, err)
		})

		return p
	}

	confirm := func(garage string, typ domain.Type, content []byte) (*domain.Result, error) {
		t.Helper()

		var (
			result *domain.Result
			cerr   error
		)

		// A failed import is a failed request: its transaction rolls back.
		_ = txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garage, UserID: userID}, func(ctx context.Context) error {
			ctx = access.WithAccess(ctx, &access.Access{GarageID: garage, UserID: userID, OwnerUserID: userID, Owner: true, Active: true})
			result, cerr = imports.Confirm(ctx, request(garage, typ, content, all))

			return cerr
		})

		return result, cerr
	}

	// ---- the 500-row parts file with 5 bad rows ----
	bad := partsFile(500, "A")
	bad[3][template.ColName] = ""                       // row 5: no name
	bad[20][template.ColSKU] = bad[19][template.ColSKU] // row 22: SKU used twice in the file
	bad[40][template.ColSupplier] = "Nobody"            // row 42: unknown supplier (part 40 has opening stock)
	bad[60][template.ColBarcodes] = "bad_code"          // row 62: character a barcode cannot hold
	bad[81][template.ColOpeningQuantity] = "many"       // row 83: not a number (part 81 has opening stock)
	badFile := importWorkbook(t, domain.TypeParts, bad)

	t.Run("the preview reports every bad row at once and the confirm writes nothing", func(t *testing.T) {
		p := preview(garageID, domain.TypeParts, badFile)

		assert.False(t, p.CanConfirm)
		assert.Equal(t, 500, p.TotalRows)
		assert.Equal(t, 495, p.ValidRows)
		require.Len(t, p.Errors, 5, "%+v", p.Errors)

		rows := make([]int, 0, 5)
		for _, e := range p.Errors {
			rows = append(rows, e.Row)
		}

		assert.Equal(t, []int{5, 22, 42, 62, 83}, rows)
		assert.Equal(t, domain.CodeRequiredValue, p.Errors[0].Code)
		assert.Equal(t, template.ColName, p.Errors[0].Column)
		assert.Equal(t, domain.CodeSKUDuplicate, p.Errors[1].Code)
		assert.Equal(t, domain.CodeUnknownSupplier, p.Errors[2].Code)
		assert.Equal(t, domain.CodeBarcodeInvalid, p.Errors[3].Code)
		assert.Equal(t, domain.CodeInvalidQuantity, p.Errors[4].Code)

		_, err := confirm(garageID, domain.TypeParts, badFile)
		assert.Equal(t, "IMPORT_FILE_INVALID", reasonOfError(err))

		assert.Zero(t, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, garageID))
		assert.Zero(t, count(`SELECT count(*) FROM units WHERE garage_id = $1`, garageID), "not even the unit of a good row")
		assert.Zero(t, count(`SELECT count(*) FROM import_runs WHERE garage_id = $1`, garageID))
	})

	good := partsFile(500, "A")
	goodFile := importWorkbook(t, domain.TypeParts, good)

	var receiptNumber string

	t.Run("the fixed file imports parts, units, barcodes and opening stock", func(t *testing.T) {
		p := preview(garageID, domain.TypeParts, goodFile)
		require.True(t, p.CanConfirm, "%+v", p.Errors)

		result, err := confirm(garageID, domain.TypeParts, goodFile)
		require.NoError(t, err)

		assert.Equal(t, 500, result.RowsImported)
		assert.Equal(t, 500, result.Counts["parts"])
		assert.Equal(t, 550, result.Counts["barcodes"], "one per part and one per case")
		assert.Equal(t, 428, result.Counts["receipt_lines"], "500 parts less the 72 whose index is a multiple of seven")
		assert.NotEmpty(t, result.DocumentID)

		receiptNumber = result.DocumentNumber

		assert.Equal(t, 500, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, garageID))
		assert.Equal(t, 2, count(`SELECT count(*) FROM units WHERE garage_id = $1`, garageID), "Piece and Case")
		assert.Equal(t, 1, count(`SELECT count(*) FROM part_categories WHERE garage_id = $1`, garageID))
		assert.Equal(t, 550, count(`SELECT count(*) FROM part_barcodes WHERE garage_id = $1`, garageID))
		assert.Equal(t, 50, count(`SELECT count(*) FROM part_barcodes WHERE garage_id = $1 AND unit_id IS NOT NULL`, garageID))
		assert.Equal(t, 1, count(`SELECT count(*) FROM import_runs WHERE garage_id = $1 AND document_id = $2`, garageID, result.DocumentID))
		assert.Equal(t, 1, count(`SELECT count(*) FROM audit_log WHERE garage_id = $1 AND action = 'import.completed'`, garageID))

		inGarage(garageID, func(ctx context.Context) {
			receipt, err := inv.uc.GetGoodsReceipt(ctx, garageID, result.DocumentID)
			require.NoError(t, err)
			assert.Equal(t, "completed", string(receipt.Status), "opening stock is posted, not left as a draft")
			assert.Len(t, receipt.Lines, 428)

			for _, line := range receipt.Lines {
				require.NotNil(t, line.SupplierID, "every line carries its supplier")
			}

			// Part 3: quantity 3%5+1 = 4 in stock; part 0 has none.
			level, err := inv.uc.GetStockLevel(ctx, garageID, partIDBySKU(t, pg, garageID, "A-0003"))
			require.NoError(t, err)
			assert.Equal(t, "4", level.QuantityOnHand)

			level, err = inv.uc.GetStockLevel(ctx, garageID, partIDBySKU(t, pg, garageID, "A-0000"))
			require.NoError(t, err)
			assert.Equal(t, "0", level.QuantityOnHand)
		})
	})

	t.Run("the same file cannot be imported twice", func(t *testing.T) {
		_, err := confirm(garageID, domain.TypeParts, goodFile)
		assert.Equal(t, "IMPORT_FILE_ALREADY_IMPORTED", reasonOfError(err))
		assert.Equal(t, 500, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, garageID))
		assert.Equal(t, 1, count(`SELECT count(*) FROM import_runs WHERE garage_id = $1`, garageID))

		// The same SKUs in another file are now duplicates of existing parts.
		again := preview(garageID, domain.TypeParts, importWorkbook(t, domain.TypeParts, good[:3]))
		assert.False(t, again.CanConfirm)
		assert.Equal(t, domain.CodeSKUAlreadyExists, again.Errors[0].Code)
	})

	t.Run("a scanned case barcode finds its part and its unit, in any case", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			part, code, err := catalogue.uc.LookupPartByBarcode(ctx, garageID, "79A000010")
			require.NoError(t, err)
			assert.Equal(t, "A-0010", part.SKU)
			require.NotNil(t, code.UnitID)
			assert.Equal(t, "Case", code.UnitName)

			part, _, err = catalogue.uc.LookupPartByBarcode(ctx, garageID, strings.ToLower("79A000010"))
			require.NoError(t, err)
			assert.Equal(t, "A-0010", part.SKU)

			page, err := catalogue.uc.ListParts(ctx, catalogueUsecase.ListPartsRequest{GarageID: garageID, Query: "79A000010"})
			require.NoError(t, err)
			require.Len(t, page.Items, 1, "the part list finds a part by an exact barcode")
			assert.Equal(t, "A-0010", page.Items[0].SKU)
		})
	})

	t.Run("a packaging unit with a barcode can be edited, but not removed", func(t *testing.T) {
		partID := partIDBySKU(t, pg, garageID, "A-0010")

		inGarage(garageID, func(ctx context.Context) {
			part, err := catalogue.uc.GetPart(ctx, garageID, partID)
			require.NoError(t, err)
			require.Len(t, part.PackagingUnits, 1)

			caseUnit := part.PackagingUnits[0]

			// The case keeps its barcode while its factor changes: the table is
			// updated in place, not deleted and rewritten.
			updated, err := catalogue.uc.SetPartUnits(ctx, garageID, partID, userID,
				[]catalogueDomain.PackagingUnit{{UnitID: caseUnit.UnitID, Factor: "24"}})
			require.NoError(t, err)
			assert.Equal(t, "24", updated.PackagingUnits[0].Factor)
			assert.Len(t, updated.Barcodes, 2, "the barcodes survived")

			_, err = catalogue.uc.SetPartUnits(ctx, garageID, partID, userID, nil)
			assert.Equal(t, "PACKAGING_UNIT_HAS_BARCODE", reasonOfError(err))

			_, err = catalogue.uc.SetPartUnits(ctx, garageID, partID, userID,
				[]catalogueDomain.PackagingUnit{{UnitID: caseUnit.UnitID, Factor: "12"}})
			require.NoError(t, err, "back to the original factor for the scenarios that follow")
		})
	})

	t.Run("garages are isolated, and a barcode is only unique within its garage", func(t *testing.T) {
		inGarage(otherGarageID, func(ctx context.Context) {
			_, _, err := catalogue.uc.LookupPartByBarcode(ctx, otherGarageID, "79A000010")
			require.Error(t, err, "another garage's code is not found")
		})

		other := importWorkbook(t, domain.TypeParts, partsFile(20, "A"))
		p := preview(otherGarageID, domain.TypeParts, other)
		require.True(t, p.CanConfirm, "%+v", p.Errors)

		_, err := confirm(otherGarageID, domain.TypeParts, other)
		require.NoError(t, err, "the same SKUs and barcodes are free in garage B")
		assert.Equal(t, 20, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, otherGarageID))
		assert.Equal(t, 500, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, garageID))
	})

	t.Run("a failure after the writes rolls the whole import back", func(t *testing.T) {
		file := importWorkbook(t, domain.TypeParts, partsFile(30, "B"))

		err := txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, func(ctx context.Context) error {
			ctx = access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true})

			_, err := imports.Confirm(ctx, request(garageID, domain.TypeParts, file, all))
			require.NoError(t, err)

			return assert.AnError // the request fails after the import wrote everything
		})
		require.ErrorIs(t, err, assert.AnError)

		assert.Equal(t, 500, count(`SELECT count(*) FROM parts WHERE garage_id = $1`, garageID), "none of the 30 parts survived")
		assert.Equal(t, 1, count(`SELECT count(*) FROM import_runs WHERE garage_id = $1`, garageID), "and the file can be imported again")
	})

	t.Run("a goods receipt is imported as a draft, with lines found by barcode", func(t *testing.T) {
		file := importWorkbook(t, domain.TypeGoodsReceipts, []map[string]string{
			{
				template.ColSKU: "79A000010", template.ColQuantity: "2", template.ColSupplier: "ACME", template.ColUnitCost: "12000",
				template.ColDeliveryNote: "DN-77", template.ColReceiptDate: "2026-10-01",
			},
			{template.ColSKU: "A-0003", template.ColQuantity: "5", template.ColSupplier: "ACME"},
			{template.ColSKU: "89A000004", template.ColQuantity: "1", template.ColSupplier: "ACME", template.ColUnit: "pc"},
		})

		p := preview(garageID, domain.TypeGoodsReceipts, file)
		require.True(t, p.CanConfirm, "%+v", p.Errors)

		result, err := confirm(garageID, domain.TypeGoodsReceipts, file)
		require.NoError(t, err)
		assert.Equal(t, 3, result.Counts["lines"])
		assert.NotEqual(t, receiptNumber, result.DocumentNumber)

		inGarage(garageID, func(ctx context.Context) {
			receipt, err := inv.uc.GetGoodsReceipt(ctx, garageID, result.DocumentID)
			require.NoError(t, err)
			assert.Equal(t, "draft", string(receipt.Status), "the user reviews and completes it")
			assert.Equal(t, "DN-77", receipt.DeliveryNoteNumber)
			require.Len(t, receipt.Lines, 3)

			require.NotNil(t, receipt.Lines[0].UnitID, "the case barcode put the quantity in cases")
			assert.Equal(t, "2", receipt.Lines[0].EnteredQuantity)
			assert.Equal(t, "24", receipt.Lines[0].Quantity, "2 cases of 12")
			assert.Nil(t, receipt.Lines[1].UnitID)
		})
	})

	t.Run("a stock count is imported as a draft, with lines found by barcode", func(t *testing.T) {
		file := importWorkbook(t, domain.TypeStockCounts, []map[string]string{
			{template.ColName: "Shelf 1", template.ColSKU: "79A000010", template.ColCountedQuantity: "3"},
			{template.ColSKU: "A-0003", template.ColCountedQuantity: "4"},
		})

		result, err := confirm(garageID, domain.TypeStockCounts, file)
		require.NoError(t, err)

		inGarage(garageID, func(ctx context.Context) {
			stockCount, err := inv.uc.GetStockCount(ctx, garageID, result.DocumentID)
			require.NoError(t, err)
			assert.Equal(t, "Shelf 1", stockCount.Name)
			assert.Equal(t, "draft", string(stockCount.Status))
			require.Len(t, stockCount.Lines, 2)

			byPart := map[string]int{}
			for i, line := range stockCount.Lines {
				byPart[line.PartSKU] = i
			}

			caseLine := stockCount.Lines[byPart["A-0010"]]
			require.NotNil(t, caseLine.UnitID, "counted in cases")
			require.NotNil(t, caseLine.Counted)
			assert.Equal(t, "36", *caseLine.Counted, "3 cases of 12 in the base unit")
		})
	})

	t.Run("files over the limits are refused as a whole", func(t *testing.T) {
		p := preview(garageID, domain.TypeParts, importWorkbook(t, domain.TypeParts, partsFile(domain.MaxRows+1, "L")))
		assert.False(t, p.CanConfirm)
		require.NotEmpty(t, p.Errors)
		assert.Equal(t, domain.CodeTooManyRows, p.Errors[0].Code)
		assert.Zero(t, p.Errors[0].Row)

		p = preview(garageID, domain.TypeParts, []byte("not a workbook"))
		assert.False(t, p.CanConfirm)
		assert.Equal(t, domain.CodeInvalidFile, p.Errors[0].Code)
	})

	t.Run("a role without the permission is told so, and a column it may not use is refused", func(t *testing.T) {
		file := importWorkbook(t, domain.TypeParts, partsFile(10, "P"))

		var p *domain.Preview

		inGarage(garageID, func(ctx context.Context) {
			var err error

			p, err = imports.Preview(ctx, request(garageID, domain.TypeParts, file, importUsecase.Permissions{CatalogueWrite: true}))
			require.NoError(t, err)
		})

		assert.False(t, p.CanConfirm)

		columns := map[string]bool{}

		for _, e := range p.Errors {
			if e.Row == 0 {
				assert.Equal(t, domain.CodePermissionRequired, e.Code)

				columns[e.Column] = true
			}
		}

		assert.True(t, columns[template.ColOpeningQuantity], "opening stock needs inventory write")
		assert.True(t, columns[template.ColUnitCost], "cost needs inventory cost read")
	})
}

// TestDataImportAtTheLimit imports the largest file allowed: 5,000 parts with
// opening stock, then a 5,000-line stock count, each inside a time budget.
func TestDataImportAtTheLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("large import")
	}

	pg := pgtest.StartMigrated(t)
	userID, garageID := uuid.NewString(), uuid.NewString()

	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, []any{userID}},
		{`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'A')`, []any{garageID, userID}},
		{`INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $2, 'ACME')`, []any{uuid.NewString(), garageID}},
	} {
		_, err := pg.Admin.ExecContext(t.Context(), stmt.query, stmt.args...)
		require.NoError(t, err)
	}

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	entities := &fileEntities{}
	files := buildFiles(pool, store, tenantRepo.NewPostgresGarageRepository(pool), catalogueRepository.NewPostgresRepository(pool), entities)
	usage := &catalogueUsage{}
	catalogue := buildCatalogue(pool, files, usage)
	entities.parts = catalogue.uc

	supplierRepo := supplierRepository.NewPostgresRepository(pool)
	inv := buildInventory(pool, outbox.NewEventPublisher(pool), inventoryParts{partQuantities{uc: catalogue.uc}},
		inventoryGarages{garages: tenantRepo.NewPostgresGarageRepository(pool)},
		inventoryDirectory{suppliers: supplierRepo, users: identityRepo.NewPostgresUserRepository(pool)},
		audit.NewLogger(pool))
	usage.inventory = inv.repo
	usage.quotes, usage.repairOrders = quoteRepository.NewPostgresRepository(pool), repairorderRepository.NewPostgresRepository(pool)

	imports := buildDataImport(pool, catalogue.uc, inv.uc, supplierRepo, audit.NewLogger(pool)).uc
	perms := importUsecase.Permissions{CatalogueWrite: true, InventoryWrite: true, InventoryCostRead: true, InventoryCount: true}

	confirm := func(typ domain.Type, content []byte) (*domain.Result, time.Duration) {
		t.Helper()

		var result *domain.Result

		started := time.Now()

		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, func(ctx context.Context) error {
			ctx = access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true})

			var err error

			result, err = imports.Confirm(ctx, importUsecase.Request{
				GarageID: garageID, ActorID: userID, Type: typ, FileName: "big.xlsx", Content: content, Permissions: perms,
			})

			return err
		}))

		return result, time.Since(started)
	}

	size := domain.MaxRows
	if v := os.Getenv("IMPORT_LIMIT_ROWS"); v != "" {
		size, _ = strconv.Atoi(v)
	}

	parts := partsFile(size, "Z")
	result, took := confirm(domain.TypeParts, importWorkbook(t, domain.TypeParts, parts))
	t.Logf("5,000 parts with opening stock: %s", took)
	assert.Equal(t, size, result.RowsImported)
	assert.Less(t, took, 90*time.Second, "a full parts file must import in a request-sized time")

	lines := make([]map[string]string, 0, size)
	for _, row := range parts {
		lines = append(lines, map[string]string{template.ColSKU: row[template.ColSKU], template.ColCountedQuantity: "1"})
	}

	result, took = confirm(domain.TypeStockCounts, importWorkbook(t, domain.TypeStockCounts, lines))
	t.Logf("5,000-line stock count: %s", took)
	assert.Equal(t, size, result.RowsImported)
	assert.Less(t, took, 90*time.Second, "a full stock count must import in a request-sized time")
}

// partIDBySKU reads a part id straight from the schema.
func partIDBySKU(t *testing.T, pg *pgtest.Postgres, garageID, sku string) string {
	t.Helper()

	var id string

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(),
		`SELECT id FROM parts WHERE garage_id = $1 AND sku = $2`, garageID, sku).Scan(&id))

	return id
}
