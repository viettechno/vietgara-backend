// Package domain contains the pure business types of the Data Import module
// (FR-INV-14): what an Excel file imports, the row errors a preview reports
// and the limits of an upload. The module writes through the same documents
// as the screens (parts through Catalogue, opening stock through a completed
// goods receipt) and owns only the import_runs history; see Release 7.2.
package domain

import (
	"errors"
	"strings"
)

// Type names what an Excel file imports (ImportType in the proto).
type Type string

const (
	// TypeParts imports parts with units, category, brand, barcodes and an
	// optional opening quantity and cost.
	TypeParts Type = "parts"
	// TypeGoodsReceipts imports one draft goods receipt with its lines.
	TypeGoodsReceipts Type = "goods_receipts"
	// TypeStockCounts imports one draft stock count with its counted
	// quantities.
	TypeStockCounts Type = "stock_counts"
)

// ParseType maps the API value to a Type; ok is false for anything else.
func ParseType(s string) (Type, bool) {
	switch Type(s) {
	case TypeParts, TypeGoodsReceipts, TypeStockCounts:
		return Type(s), true
	default:
		return "", false
	}
}

// Upload limits. A larger file or one with more data rows is reported as a
// row-0 error and never imported.
const (
	MaxFileBytes = 5 << 20
	MaxRows      = 5000
	// MaxErrors caps the errors of one preview; errors_truncated says more
	// were found.
	MaxErrors = 1000
	// MaxSampleRows is the length of the preview table.
	MaxSampleRows = 20
	// MaxBarcodesPerPart is the Catalogue's limit of barcodes on one part.
	MaxBarcodesPerPart = 20
)

// Row-error codes, stable for clients.
const (
	CodeFileTooLarge       = "FILE_TOO_LARGE"
	CodeTooManyRows        = "TOO_MANY_ROWS"
	CodeMissingColumn      = "MISSING_COLUMN"
	CodeInvalidFile        = "INVALID_FILE"
	CodeFileEmpty          = "FILE_EMPTY"
	CodePermissionRequired = "PERMISSION_REQUIRED"
	CodeRequiredValue      = "REQUIRED_VALUE_MISSING"
	CodeInvalidValue       = "INVALID_VALUE"
	CodeSKUDuplicate       = "SKU_DUPLICATE"
	CodeSKUAlreadyExists   = "SKU_ALREADY_EXISTS"
	CodeUnknownSKU         = "UNKNOWN_SKU"
	CodePartInactive       = "PART_INACTIVE"
	CodeBarcodeInvalid     = "BARCODE_INVALID"
	CodeBarcodeAlreadyUsed = "BARCODE_ALREADY_USED"
	CodeDuplicateBarcode   = "BARCODE_DUPLICATE"
	CodeInvalidQuantity    = "INVALID_QUANTITY"
	CodeInvalidCost        = "INVALID_COST"
	CodeInvalidDate        = "INVALID_DATE"
	CodeUnknownSupplier    = "UNKNOWN_SUPPLIER"
	CodeSupplierRequired   = "SUPPLIER_REQUIRED"
	CodeUnknownUnit        = "UNKNOWN_UNIT"
	CodeUnitNotForPart     = "UNIT_NOT_FOR_PART"
	CodeInvalidUnit        = "INVALID_UNIT"
	CodeInvalidPackaging   = "INVALID_PACKAGING_UNIT"
	CodeInvalidCategory    = "INVALID_CATEGORY"
	CodeUnitMismatch       = "UNIT_MISMATCH"
	CodeHeaderMismatch     = "HEADER_MISMATCH"
)

// RowError is one problem found in a file. Row 0 is a problem of the whole
// file; data rows are numbered as the sheet shows them (the header is row 1,
// so the first data row is 2).
type RowError struct {
	Row     int
	Column  string
	Code    string
	Message string
}

// SampleRow is one row of the preview table, in column order.
type SampleRow struct {
	Row   int
	Cells []string
}

// Preview is the result of checking a file without writing anything.
type Preview struct {
	Type            Type
	TotalRows       int
	ValidRows       int
	Errors          []RowError
	ErrorsTruncated bool
	Columns         []string
	SampleRows      []SampleRow
	Counts          map[string]int
	CanConfirm      bool
}

// Result is what a confirmed import created.
type Result struct {
	Type           Type
	RowsImported   int
	Counts         map[string]int
	DocumentID     string
	DocumentNumber string
}

// Sentinel errors of the module.
var (
	ErrFileInvalid         = errors.New("the file is not a readable Excel workbook")
	ErrFileTooLarge        = errors.New("the file is larger than 5 MB")
	ErrTooManyRows         = errors.New("the file has more than 5000 data rows")
	ErrImportFileInvalid   = errors.New("the file has errors and was not imported")
	ErrFileAlreadyImported = errors.New("this file was already imported")
	ErrPermissionRequired  = errors.New("your role does not allow this import")
	ErrUnsupportedType     = errors.New("unsupported import type")
)

// NormalizeHeader makes a header comparable across spacing and case.
func NormalizeHeader(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}
