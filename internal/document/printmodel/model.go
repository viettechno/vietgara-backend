// Package printmodel holds the print model of every printable document
// (FR-DOC-04): the data a template may print, and nothing else. A model
// carries the garage's legal header from the document's snapshot, the
// billing party, the parties and numbers, the lines with their quantities in
// the line's unit, amounts as integer VND and dates as text. It has no
// layout and no dependency, so any template, now and in a later phase, can
// work from it. The JSON of each model is pinned by a golden test: change a
// field only together with a new template version.
package printmodel

import "slices"

// DocumentType is a printable document.
type DocumentType string

// The printable documents.
const (
	TypeQuote          DocumentType = "quote"
	TypeRepairOrder    DocumentType = "repair_order"
	TypeSettlement     DocumentType = "settlement"
	TypePaymentVoucher DocumentType = "payment_voucher"
	TypeGoodsReceipt   DocumentType = "goods_receipt"
	TypeStockIssue     DocumentType = "stock_issue"
)

// Types lists every printable document, in the order the settings show them.
func Types() []DocumentType {
	return []DocumentType{TypeQuote, TypeRepairOrder, TypeSettlement, TypePaymentVoucher, TypeGoodsReceipt, TypeStockIssue}
}

// Valid reports whether t is a printable document.
func (t DocumentType) Valid() bool {
	return slices.Contains(Types(), t)
}

// Paper is the paper a document prints on.
type Paper string

// The paper sizes: A4 is 210 x 297 mm, A5 148 x 210 mm.
const (
	A4 Paper = "A4"
	A5 Paper = "A5"
)

// Model is a document's print model.
type Model interface {
	Type() DocumentType
}

// GarageHeader is the letterhead shown at the top of every document: the
// garage's legal identity (FR-TEN-06), with its logo when it has one.
type GarageHeader struct {
	// Name is the legal name, or the trading name when none is set.
	Name string `json:"name"`
	// Address is the legal address, or the trading address.
	Address string `json:"address"`
	Phone   string `json:"phone"`
	// TaxCode, Representative and Bank are printed when set; Bank is one
	// line such as "Vietcombank - 0011 2233 - Cong ty An Phat".
	TaxCode        string `json:"taxCode"`
	Representative string `json:"representative"`
	Bank           string `json:"bank"`
	// Logo is the image content (PNG or JPEG); empty prints no logo.
	Logo []byte `json:"logo,omitempty"`
}

// BillTo is who a document is billed to (FR-CUS-07), printed under the
// header when the document was billed to a tax profile.
type BillTo struct {
	Name    string `json:"name"`
	TaxID   string `json:"taxId"`
	Address string `json:"address"`
}

// IsZero reports whether the document is a retail receipt.
func (b BillTo) IsZero() bool { return b.Name == "" }

// DocumentLine is one line item row (a quote line, a repair order line).
type DocumentLine struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   int64  `json:"unitPrice"`
	LineTotal   int64  `json:"lineTotal"`
}

// PaymentRow is one payment line on a settlement receipt.
type PaymentRow struct {
	Date   string `json:"date"`
	Method string `json:"method"`
	Amount int64  `json:"amount"`
}

// GoodsReceiptRow is one line of a goods receipt document.
type GoodsReceiptRow struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	// Unit is the unit Quantity is typed in.
	Unit     string `json:"unit"`
	Supplier string `json:"supplier"`
	// UnitCost and Amount are VND; they are left out of the document when
	// ShowCost is false.
	UnitCost int64 `json:"unitCost"`
	Amount   int64 `json:"amount"`
}

// StockIssueRow is one line of a stock issue document.
type StockIssueRow struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

// Quote is the print model of a quote (FR-DOC-01).
type Quote struct {
	Garage       GarageHeader   `json:"garage"`
	BillTo       BillTo         `json:"billTo"`
	Number       string         `json:"number"`
	Status       string         `json:"status"`
	CustomerName string         `json:"customerName"`
	VehiclePlate string         `json:"vehiclePlate"`
	Notes        string         `json:"notes"`
	Lines        []DocumentLine `json:"lines"`
	TotalAmount  int64          `json:"totalAmount"`
}

// Type implements Model.
func (Quote) Type() DocumentType { return TypeQuote }

// RepairOrder is the print model of a repair order (FR-DOC-01).
type RepairOrder struct {
	Garage       GarageHeader   `json:"garage"`
	BillTo       BillTo         `json:"billTo"`
	Number       string         `json:"number"`
	Status       string         `json:"status"`
	CustomerName string         `json:"customerName"`
	VehiclePlate string         `json:"vehiclePlate"`
	Lines        []DocumentLine `json:"lines"`
	TotalAmount  int64          `json:"totalAmount"`
}

// Type implements Model.
func (RepairOrder) Type() DocumentType { return TypeRepairOrder }

// Settlement is the print model of a settlement receipt (FR-DOC-01).
type Settlement struct {
	Garage       GarageHeader `json:"garage"`
	BillTo       BillTo       `json:"billTo"`
	Number       string       `json:"number"`
	Status       string       `json:"status"`
	CustomerName string       `json:"customerName"`
	VehiclePlate string       `json:"vehiclePlate"`
	TotalAmount  int64        `json:"totalAmount"`
	PaidAmount   int64        `json:"paidAmount"`
	Payments     []PaymentRow `json:"payments"`
}

// Type implements Model.
func (Settlement) Type() DocumentType { return TypeSettlement }

// PaymentVoucher is the print model of a payment voucher (phiếu thu,
// FR-BIL-05).
type PaymentVoucher struct {
	Garage GarageHeader `json:"garage"`
	Number string       `json:"number"`
	// Date is the day the voucher was issued, "02/01/2006".
	Date             string `json:"date"`
	PayerName        string `json:"payerName"`
	PayerAddress     string `json:"payerAddress"`
	Reason           string `json:"reason"`
	Amount           int64  `json:"amount"`
	Method           string `json:"method"`
	SettlementNumber string `json:"settlementNumber"`
	CollectorName    string `json:"collectorName"`
	// Voided marks a voucher that no longer counts; VoidReason says why.
	Voided     bool   `json:"voided"`
	VoidReason string `json:"voidReason"`
}

// Type implements Model.
func (PaymentVoucher) Type() DocumentType { return TypePaymentVoucher }

// GoodsReceipt is the print model of a goods receipt (FR-INV-06).
type GoodsReceipt struct {
	Garage             GarageHeader      `json:"garage"`
	Number             string            `json:"number"`
	Status             string            `json:"status"`
	Date               string            `json:"date"`
	DeliveryNoteNumber string            `json:"deliveryNoteNumber"`
	Notes              string            `json:"notes"`
	Lines              []GoodsReceiptRow `json:"lines"`
	// ShowCost prints the cost columns and the total; a caller without
	// inventory.cost.read gets quantities only (FR-INV-08).
	ShowCost    bool  `json:"showCost"`
	TotalAmount int64 `json:"totalAmount"`
}

// Type implements Model.
func (GoodsReceipt) Type() DocumentType { return TypeGoodsReceipt }

// StockIssue is the print model of a stock issue (phiếu xuất kho,
// FR-INV-20). It prints quantities only: no cost is on the document.
type StockIssue struct {
	Garage            GarageHeader    `json:"garage"`
	Number            string          `json:"number"`
	Status            string          `json:"status"`
	RepairOrderNumber string          `json:"repairOrderNumber"`
	PostedAt          string          `json:"postedAt"`
	PostedBy          string          `json:"postedBy"`
	Lines             []StockIssueRow `json:"lines"`
}

// Type implements Model.
func (StockIssue) Type() DocumentType { return TypeStockIssue }
