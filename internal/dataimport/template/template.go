// Package template renders the downloadable Excel template of each import
// type (FR-INV-14). The Data sheet carries exactly the headers the reader
// expects; the Instructions sheet explains every column and the accepted
// formatting.
package template

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
)

// Column headers. They are the contract between the template and the reader.
const (
	ColSKU             = "SKU"
	ColName            = "Name"
	ColBaseUnit        = "Base unit"
	ColBaseUnitAbbr    = "Base unit abbreviation"
	ColCategory        = "Category"
	ColBrand           = "Brand"
	ColBarcodes        = "Barcodes"
	ColPackagingUnits  = "Packaging units"
	ColOpeningQuantity = "Opening quantity"
	ColUnitCost        = "Unit cost"
	ColSupplier        = "Supplier"
	ColReceiptDate     = "Receipt date"
	ColDeliveryNote    = "Delivery note"
	ColNotes           = "Notes"
	ColQuantity        = "Quantity"
	ColUnit            = "Unit"
	ColCountedQuantity = "Counted quantity"
)

const (
	dataSheet         = "Data"
	instructionsSheet = "Instructions"
)

var columns = map[domain.Type][]string{
	domain.TypeParts: {
		ColSKU, ColName, ColBaseUnit, ColBaseUnitAbbr, ColCategory, ColBrand, ColBarcodes,
		ColPackagingUnits, ColOpeningQuantity, ColUnitCost, ColSupplier,
	},
	domain.TypeGoodsReceipts: {
		ColReceiptDate, ColDeliveryNote, ColNotes, ColSKU, ColQuantity, ColUnit, ColUnitCost, ColSupplier,
	},
	domain.TypeStockCounts: {ColName, ColNotes, ColSKU, ColCountedQuantity, ColUnit},
}

// instruction explains one column in English and Vietnamese.
type instruction struct {
	column string
	en     string
	vi     string
}

var instructions = map[domain.Type][]instruction{
	domain.TypeParts: {
		{
			ColSKU, "Required. The part code; unique in the garage and in this file (for example BRK-001).",
			"Bắt buộc. Mã phụ tùng, không trùng trong gara và trong file này (ví dụ BRK-001).",
		},
		{ColName, "Required. The part name.", "Bắt buộc. Tên phụ tùng."},
		{
			ColBaseUnit, "Required. The base unit name (for example Piece). A missing unit is created.",
			"Bắt buộc. Tên đơn vị cơ bản (ví dụ Cái). Đơn vị chưa có sẽ được tạo mới.",
		},
		{
			ColBaseUnitAbbr, "Required. The base unit abbreviation, at most 20 characters (for example pc).",
			"Bắt buộc. Ký hiệu đơn vị cơ bản, tối đa 20 ký tự (ví dụ cái).",
		},
		{
			ColCategory, "Optional. A part category; a missing one is created at the top level.",
			"Không bắt buộc. Nhóm phụ tùng; nhóm chưa có sẽ được tạo ở cấp cao nhất.",
		},
		{ColBrand, "Optional. The brand.", "Không bắt buộc. Thương hiệu."},
		{
			ColBarcodes, "Optional. One or more barcodes separated by a semicolon (;). Letters, digits, space and - . / + $ %; up to 64 " +
				"characters each and 20 codes per part. A barcode that stands for a packaging unit is written Code@Unit, for example " +
				"8934563000123;8934563000999@Case, and that unit must be listed under Packaging units.",
			"Không bắt buộc. Một hoặc nhiều mã vạch cách nhau bằng dấu chấm phẩy (;). Chữ, số, dấu cách và - . / + $ %; tối đa 64 ký tự " +
				"mỗi mã, 20 mã mỗi phụ tùng. Mã vạch của đơn vị đóng gói viết dạng Mã@Đơn vị, ví dụ " +
				"8934563000123;8934563000999@Thùng, và đơn vị đó phải có trong cột Packaging units.",
		},
		{
			ColPackagingUnits, "Optional. Packaging units as Name=Factor separated by a semicolon (;), for example Case=24;Box=6. " +
				"A missing unit is created with its name as abbreviation.",
			"Không bắt buộc. Đơn vị đóng gói dạng Tên=Hệ số, cách nhau bằng dấu chấm phẩy (;), ví dụ Thùng=24;Hộp=6. " +
				"Đơn vị chưa có sẽ được tạo mới, lấy tên làm ký hiệu.",
		},
		{
			ColOpeningQuantity, "Optional. The quantity already on hand, in the base unit. Needs the inventory write permission and, " +
				"when above zero, a supplier.",
			"Không bắt buộc. Số lượng tồn đầu kỳ theo đơn vị cơ bản. Cần quyền ghi kho và, khi lớn hơn 0, phải có nhà cung cấp.",
		},
		{
			ColUnitCost, "Optional. VND per base unit for the opening quantity. Needs the inventory cost permission.",
			"Không bắt buộc. Giá vốn (VND) trên một đơn vị cơ bản của tồn đầu kỳ. Cần quyền xem giá vốn.",
		},
		{
			ColSupplier, "Required when there is an opening quantity: the exact name of an existing supplier.",
			"Bắt buộc khi có tồn đầu kỳ: đúng tên một nhà cung cấp đã có.",
		},
	},
	domain.TypeGoodsReceipts: {
		{
			ColReceiptDate, "Optional. The receipt date as YYYY-MM-DD; today when empty. Every row that fills it must use the same date.",
			"Không bắt buộc. Ngày nhập kho dạng YYYY-MM-DD; để trống là hôm nay. Các dòng có điền phải cùng một ngày.",
		},
		{
			ColDeliveryNote, "Optional. The supplier's delivery note number. Every row that fills it must use the same value.",
			"Không bắt buộc. Số phiếu giao hàng của nhà cung cấp. Các dòng có điền phải giống nhau.",
		},
		{
			ColNotes, "Optional. Notes for the whole receipt. Every row that fills it must use the same text.",
			"Không bắt buộc. Ghi chú cho cả phiếu. Các dòng có điền phải giống nhau.",
		},
		{
			ColSKU, "Required. The SKU of an existing part, or one of its barcodes.",
			"Bắt buộc. Mã (SKU) của phụ tùng đã có, hoặc một trong các mã vạch của nó.",
		},
		{ColQuantity, "Required. The received quantity, above zero.", "Bắt buộc. Số lượng nhận, lớn hơn 0."},
		{
			ColUnit, "Optional. A unit name or abbreviation of the part (for example case); empty means the base unit, or the unit " +
				"of the barcode when a barcode was used.",
			"Không bắt buộc. Tên hoặc ký hiệu đơn vị của phụ tùng (ví dụ thùng); để trống là đơn vị cơ bản, " +
				"hoặc đơn vị của mã vạch nếu dùng mã vạch.",
		},
		{
			ColUnitCost, "Optional. VND per entered unit. Needs the inventory cost permission.",
			"Không bắt buộc. Giá vốn (VND) trên một đơn vị đã nhập. Cần quyền xem giá vốn.",
		},
		{ColSupplier, "Required. The exact name of an existing supplier.", "Bắt buộc. Đúng tên một nhà cung cấp đã có."},
	},
	domain.TypeStockCounts: {
		{
			ColName, "Optional. The count name; a default is used when empty. Every row that fills it must use the same name.",
			"Không bắt buộc. Tên phiếu kiểm kê; để trống sẽ dùng tên mặc định. Các dòng có điền phải giống nhau.",
		},
		{
			ColNotes, "Optional. Notes for the whole count. Every row that fills it must use the same text.",
			"Không bắt buộc. Ghi chú cho cả phiếu. Các dòng có điền phải giống nhau.",
		},
		{
			ColSKU, "Required. The SKU of an existing, active part, or one of its barcodes; each part once.",
			"Bắt buộc. Mã (SKU) của phụ tùng đang hoạt động, hoặc một trong các mã vạch của nó; mỗi phụ tùng một lần.",
		},
		{ColCountedQuantity, "Required. The counted quantity, zero or above.", "Bắt buộc. Số lượng đếm được, từ 0 trở lên."},
		{
			ColUnit, "Optional. A unit name or abbreviation of the part; empty means the base unit, or the unit of the barcode " +
				"when a barcode was used.",
			"Không bắt buộc. Tên hoặc ký hiệu đơn vị của phụ tùng; để trống là đơn vị cơ bản, hoặc đơn vị của mã vạch nếu dùng mã vạch.",
		},
	},
}

// Columns returns the Data-sheet headers of typ.
func Columns(typ domain.Type) []string {
	return columns[typ]
}

// ErrNoTemplate is returned for an import type that has no template.
var ErrNoTemplate = errors.New("no template for this import type")

// Build renders the workbook of typ and its file name.
func Build(typ domain.Type) ([]byte, string, error) {
	headers, ok := columns[typ]
	if !ok {
		return nil, "", fmt.Errorf("%w: %q", ErrNoTemplate, typ)
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	headerStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, "", fmt.Errorf("render template: %w", err)
	}

	if err := writeDataSheet(f, headers, headerStyle); err != nil {
		return nil, "", fmt.Errorf("render template: %w", err)
	}

	if err := writeInstructions(f, typ, headerStyle); err != nil {
		return nil, "", fmt.Errorf("render template: %w", err)
	}

	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", fmt.Errorf("render template: %w", err)
	}

	return buf.Bytes(), "vietgara-" + string(typ) + "-template.xlsx", nil
}

// writeDataSheet renames the first sheet to Data and fills its header row.
// Code columns are text: a numeric cell would drop the leading zeros of a SKU
// or a barcode before the reader sees them.
func writeDataSheet(f *excelize.File, headers []string, headerStyle int) error {
	if err := f.SetSheetName("Sheet1", dataSheet); err != nil {
		return err
	}

	if err := f.SetSheetRow(dataSheet, "A1", &headers); err != nil {
		return err
	}

	if err := f.SetRowStyle(dataSheet, 1, 1, headerStyle); err != nil {
		return err
	}

	if err := f.SetPanes(dataSheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}

	textStyle, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return err
	}

	for i, header := range headers {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}

		if err := f.SetColWidth(dataSheet, col, col, 24); err != nil {
			return err
		}

		if header == ColSKU || header == ColBarcodes {
			if err := f.SetColStyle(dataSheet, col, textStyle); err != nil {
				return err
			}
		}
	}

	return nil
}

// writeInstructions adds the sheet that explains every column in English and
// Vietnamese.
func writeInstructions(f *excelize.File, typ domain.Type, headerStyle int) error {
	if _, err := f.NewSheet(instructionsSheet); err != nil {
		return err
	}

	if err := f.SetSheetRow(instructionsSheet, "A1", &[]string{"Column", "How to fill it", "Cách điền"}); err != nil {
		return err
	}

	if err := f.SetRowStyle(instructionsSheet, 1, 1, headerStyle); err != nil {
		return err
	}

	for i, item := range instructions[typ] {
		row := []any{item.column, item.en, item.vi}
		if err := f.SetSheetRow(instructionsSheet, fmt.Sprintf("A%d", i+2), &row); err != nil {
			return err
		}
	}

	if err := f.SetColWidth(instructionsSheet, "A", "A", 24); err != nil {
		return err
	}

	return f.SetColWidth(instructionsSheet, "B", "C", 80)
}
