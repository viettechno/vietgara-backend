package domain

import (
	"errors"
	"math/big"
	"time"
)

// PurchaseOrderStatus is the lifecycle of a purchase order (FR-INV-16): saved
// as a draft, sent to the supplier, received in part or in full, or cancelled.
type PurchaseOrderStatus string

// Valid PurchaseOrderStatus values.
const (
	PurchaseOrderDraft             PurchaseOrderStatus = "draft"
	PurchaseOrderSent              PurchaseOrderStatus = "sent"
	PurchaseOrderPartiallyReceived PurchaseOrderStatus = "partially_received"
	PurchaseOrderReceived          PurchaseOrderStatus = "received"
	PurchaseOrderCancelled         PurchaseOrderStatus = "cancelled"
)

// IsValid reports whether s is one of the defined statuses.
func (s PurchaseOrderStatus) IsValid() bool {
	switch s {
	case PurchaseOrderDraft, PurchaseOrderSent, PurchaseOrderPartiallyReceived, PurchaseOrderReceived, PurchaseOrderCancelled:
		return true
	default:
		return false
	}
}

// PurchaseOrderLine is one part on a purchase order.
type PurchaseOrderLine struct {
	ID     string
	PartID string
	// PartSKU, PartName and BaseUnitAbbreviation are derived when reading.
	PartSKU              string
	PartName             string
	BaseUnitAbbreviation string
	// UnitID is the packaging unit EnteredQuantity is typed in; nil for the
	// base unit. UnitAbbreviation is derived.
	UnitID           *string
	UnitAbbreviation string
	// EnteredQuantity is typed in UnitID; Quantity is the derived base-unit
	// amount; ReceivedQuantity the base-unit amount delivered so far. All are
	// decimal strings.
	EnteredQuantity  string
	Quantity         string
	ReceivedQuantity string
	// UnitCost is the expected VND per entered unit; 0 means unknown.
	UnitCost int64
}

// Amount is the line's expected total, whole VND.
func (l *PurchaseOrderLine) Amount() int64 { return LineAmount(l.EnteredQuantity, l.UnitCost) }

// Outstanding is the base-unit quantity still to be delivered, never negative.
func (l *PurchaseOrderLine) Outstanding() string {
	if !Less(l.ReceivedQuantity, l.Quantity) {
		return "0"
	}

	return Subtract(l.Quantity, l.ReceivedQuantity)
}

// OutstandingEntered is the outstanding quantity in the unit the line is typed
// in, and that unit. When it cannot be written in that unit with three
// decimals without changing the amount, it is the base-unit quantity and a nil
// unit, so a receipt of it covers the line exactly. For a line in the base unit
// it is Outstanding.
func (l *PurchaseOrderLine) OutstandingEntered() (string, *string) {
	outstanding := l.Outstanding()
	if l.UnitID == nil {
		return outstanding, nil
	}

	out, ok := new(big.Rat).SetString(outstanding)
	entered, okEntered := new(big.Rat).SetString(l.EnteredQuantity)
	base, okBase := new(big.Rat).SetString(l.Quantity)

	if !ok || !okEntered || !okBase || base.Sign() == 0 || entered.Sign() == 0 {
		return outstanding, nil
	}

	typed := new(big.Rat).Mul(out, entered)
	typed.Quo(typed, base)

	rounded, _ := new(big.Rat).SetString(typed.FloatString(3)) // half away from zero

	// Back to the base unit: it must be the outstanding amount again.
	back := new(big.Rat).Mul(rounded, base)
	back.Quo(back, entered)

	if rounded.Sign() == 0 || back.Cmp(out) != 0 {
		return outstanding, nil
	}

	return trimDecimal(typed.FloatString(3)), l.UnitID
}

// PurchaseOrder is a numbered order to one supplier (FR-INV-16).
type PurchaseOrder struct {
	ID       string
	GarageID string
	// Number is rendered from the fixed purchase order numbering.
	Number string
	Status PurchaseOrderStatus
	// SupplierName, SupplierEmail are derived when reading.
	SupplierID    string
	SupplierName  string
	SupplierEmail string
	// OrderDate and ExpectedDate are "YYYY-MM-DD" in the garage's timezone;
	// ExpectedDate is empty when none is expected.
	OrderDate    string
	ExpectedDate string
	Notes        string
	Lines        []PurchaseOrderLine
	// TotalAmount is the sum of the lines' amounts; the repository fills it
	// for list pages, which carry no lines.
	TotalAmount int64
	SentAt      *time.Time
	// SentTo is the address the order was last sent to.
	SentTo      string
	CancelledAt *time.Time
	CreatedBy   *string
	UpdatedBy   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Total sums the amounts of the lines.
func (o *PurchaseOrder) Total() int64 {
	var total int64
	for i := range o.Lines {
		total += o.Lines[i].Amount()
	}

	return total
}

// CanReceive reports whether a goods receipt may be created for the order.
func (o *PurchaseOrder) CanReceive() bool {
	return o.Status == PurchaseOrderSent || o.Status == PurchaseOrderPartiallyReceived
}

// Recount sets the status from the quantities received: received once every
// line reached its quantity, partially received once any was delivered, sent
// otherwise. A draft or cancelled order keeps its status.
func (o *PurchaseOrder) Recount() {
	switch o.Status {
	case PurchaseOrderSent, PurchaseOrderPartiallyReceived, PurchaseOrderReceived:
	case PurchaseOrderDraft, PurchaseOrderCancelled:
		return
	}

	complete, started := len(o.Lines) > 0, false

	for i := range o.Lines {
		l := &o.Lines[i]
		if Less(l.ReceivedQuantity, l.Quantity) {
			complete = false
		}

		if !IsZero(l.ReceivedQuantity) {
			started = true
		}
	}

	switch {
	case complete:
		o.Status = PurchaseOrderReceived
	case started:
		o.Status = PurchaseOrderPartiallyReceived
	default:
		o.Status = PurchaseOrderSent
	}
}

// Errors of the purchase order.
var (
	ErrPurchaseOrderNotFound = errors.New("purchase order not found")
	// ErrPurchaseOrderNotDraft is returned for changing an order that is no
	// longer a draft.
	ErrPurchaseOrderNotDraft = errors.New("only a draft purchase order can be changed")
	// ErrPurchaseOrderNotSendable is returned for sending an order that is
	// neither a draft nor sent.
	ErrPurchaseOrderNotSendable = errors.New("only a draft or sent purchase order can be sent")
	// ErrPurchaseOrderNotCancellable is returned for cancelling an order that
	// is received, cancelled or has a draft goods receipt open.
	ErrPurchaseOrderNotCancellable = errors.New("the purchase order cannot be cancelled")
	// ErrPurchaseOrderNothingToReceive is returned for receiving an order that
	// is not sent or partially received.
	ErrPurchaseOrderNothingToReceive = errors.New("the purchase order has nothing to receive")
	// ErrPurchaseOrderReceiptOpen is returned while a draft goods receipt of
	// the order exists.
	ErrPurchaseOrderReceiptOpen = errors.New("the purchase order already has a draft goods receipt")
	// ErrSupplierEmailRequired is returned for sending an order to a supplier
	// with no e-mail address on file (or one that cannot be mailed).
	ErrSupplierEmailRequired = errors.New("the supplier has no e-mail address to send the order to")
	// ErrDuplicatePart is returned for a part listed twice on an order.
	ErrDuplicatePart = errors.New("a part can be on an order once")
	// ErrInvalidExpectedDate is returned for an expected date that is not
	// YYYY-MM-DD or lies in the past.
	ErrInvalidExpectedDate = errors.New("the expected date must be today or later, in the form YYYY-MM-DD")
	// ErrNotesTooLong is returned for notes over MaxNotesLength.
	ErrNotesTooLong = errors.New("notes are too long")
	// ErrMessageTooLong is returned for an e-mail message over
	// MaxOrderMessageLength.
	ErrMessageTooLong = errors.New("the message is too long")
)

// Limits of a purchase order.
const (
	MaxNotesLength        = 2000
	MaxOrderMessageLength = 1000
)

// SuggestedQuantity is the base-unit quantity that brings a part back to
// twice its reorder threshold from what is available plus what is already on
// order, at least 1; "" when it is there already (no order needed). All are
// decimal strings.
func SuggestedQuantity(threshold, available, onOrder string) string {
	target, ok := new(big.Rat).SetString(threshold)
	if !ok {
		return ""
	}

	target.Mul(target, big.NewRat(2, 1))

	for _, s := range []string{available, onOrder} {
		have, ok := new(big.Rat).SetString(orZero(s))
		if !ok {
			return ""
		}

		target.Sub(target, have)
	}

	if target.Sign() <= 0 {
		return ""
	}

	if target.Cmp(big.NewRat(1, 1)) < 0 {
		return "1"
	}

	return trimDecimal(target.FloatString(3))
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}

	return s
}
