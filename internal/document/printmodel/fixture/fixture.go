// Package fixture holds one fixed model of every printable document, for the
// tests that pin the print models and the released templates.
package fixture

import "github.com/viettechno/vietgara-backend/internal/document/printmodel"

// The customer and the vehicle every document is for.
const (
	// issuedOn is the day every fixture document is dated.
	issuedOn = "29/10/2026"
	customer = "Trần Thị Bình"
	plate    = "51F-123.45"
)

func garage() printmodel.GarageHeader {
	return printmodel.GarageHeader{
		Name: "Công ty TNHH Gara An Phát", Address: "12 Nguyễn Huệ, Quận 1, TP.HCM", Phone: "0281234567",
		TaxCode: "0301234567", Representative: "Nguyễn Văn An", Bank: "Vietcombank - 0011 2233 - An Phat",
	}
}

func lines() []printmodel.DocumentLine {
	return []printmodel.DocumentLine{
		{Description: "Thay dầu động cơ", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000},
		{Description: "Lọc gió", Quantity: "2", UnitPrice: 120_000, LineTotal: 240_000},
	}
}

func billTo() printmodel.BillTo {
	return printmodel.BillTo{Name: "Công ty Vận tải Á Châu", TaxID: "0309876543", Address: "45 Lê Lợi, Quận 1"}
}

// All returns one model of every printable document, in the order of
// printmodel.Types.
func All() []printmodel.Model {
	return []printmodel.Model{
		printmodel.Quote{
			Garage: garage(), BillTo: billTo(), Number: "BG-202610-0001", Status: "approved", CustomerName: customer,
			VehiclePlate: plate, Notes: "Bảo hành 6 tháng", Lines: lines(), TotalAmount: 490_000,
		},
		printmodel.RepairOrder{
			Garage: garage(), BillTo: billTo(), Number: "LSC-202610-0001", Status: "completed", CustomerName: customer,
			VehiclePlate: plate, Lines: lines(), TotalAmount: 490_000,
		},
		printmodel.Settlement{
			Garage: garage(), BillTo: billTo(), Number: "QT-202610-0001", Status: "partially_paid", CustomerName: customer,
			VehiclePlate: plate, TotalAmount: 490_000, PaidAmount: 200_000,
			Payments: []printmodel.PaymentRow{{Date: issuedOn, Method: "cash", Amount: 200_000}},
		},
		printmodel.PaymentVoucher{
			Garage: garage(), Number: "PT-202610-0001", Date: issuedOn, PayerName: customer, PayerAddress: "7 Hai Bà Trưng",
			Reason: "Thanh toán quyết toán", Amount: 200_000, Method: "Tiền mặt", SettlementNumber: "QT-202610-0001", CollectorName: "Lê Văn Cường",
		},
		printmodel.GoodsReceipt{
			Garage: garage(), Number: "NK-202610-0001", Status: "completed", Date: issuedOn, DeliveryNoteNumber: "GH-77", Notes: "Hàng đủ",
			Lines: []printmodel.GoodsReceiptRow{
				{SKU: "OIL-5W30", Name: "Dầu nhớt 5W-30", Quantity: "4", Unit: "L", Supplier: "Nhà cung cấp A", UnitCost: 90_000, Amount: 360_000},
			},
			ShowCost: true, TotalAmount: 360_000,
		},
		printmodel.StockIssue{
			Garage: garage(), Number: "XK-202610-0001", Status: "posted", RepairOrderNumber: "LSC-202610-0001",
			PostedAt: "29/10/2026 09:30", PostedBy: "Lê Văn Cường",
			Lines: []printmodel.StockIssueRow{{SKU: "OIL-5W30", Name: "Dầu nhớt 5W-30", Quantity: "4", Unit: "L"}},
		},
		printmodel.PaymentOutVoucher{
			Garage: garage(), Number: "PC-2026-00001", Date: issuedOn, PayeeName: "Nhà cung cấp A", PayeeAddress: "12 Lê Lợi, Quận 1",
			Reason: "Thanh toán tiền hàng theo phiếu nhập NK-202610-0001", Amount: 360_000, Method: "Chuyển khoản",
			Receipts: []printmodel.PaymentOutReceiptRow{{Number: "NK-202610-0001", Amount: 360_000}}, IssuerName: "Lê Văn Cường",
		},
		printmodel.PurchaseOrder{
			Garage: garage(), Number: "DH-2026-00001", Status: "sent", Date: issuedOn, ExpectedDate: "05/11/2026",
			SupplierName: "Nhà cung cấp A", Notes: "Giao trong giờ hành chính",
			Lines: []printmodel.PurchaseOrderRow{
				{SKU: "OIL-5W30", Name: "Dầu nhớt 5W-30", Quantity: "16", Unit: "L", UnitCost: 90_000, Amount: 1_440_000},
			},
			ShowCost: true, TotalAmount: 1_440_000,
		},
	}
}
