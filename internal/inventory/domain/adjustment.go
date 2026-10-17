package domain

import (
	"errors"
	"math/big"
	"strings"
	"time"
)

// AdjustmentStatus is the lifecycle of a write-off or a return to supplier:
// created already posted (completed), undone by cancelling it.
type AdjustmentStatus string

// Valid AdjustmentStatus values.
const (
	AdjustmentCompleted AdjustmentStatus = "completed"
	AdjustmentCancelled AdjustmentStatus = "cancelled"
)

// IsValid reports whether s is one of the defined statuses.
func (s AdjustmentStatus) IsValid() bool { return s == AdjustmentCompleted || s == AdjustmentCancelled }

// WriteOffReason is why stock is written off (FR-INV-09).
type WriteOffReason string

// Valid WriteOffReason values.
const (
	WriteOffDamaged             WriteOffReason = "damaged"
	WriteOffLost                WriteOffReason = "lost"
	WriteOffExpired             WriteOffReason = "expired"
	WriteOffInternalUse         WriteOffReason = "internal_use"
	WriteOffWarrantyReplacement WriteOffReason = "warranty_replacement"
)

// IsValid reports whether r is one of the defined reasons.
func (r WriteOffReason) IsValid() bool {
	switch r {
	case WriteOffDamaged, WriteOffLost, WriteOffExpired, WriteOffInternalUse, WriteOffWarrantyReplacement:
		return true
	default:
		return false
	}
}

// AdjustmentLine is one part of a write-off or a return to supplier.
type AdjustmentLine struct {
	ID       string
	Position int
	PartID   string
	// PartSKU, PartName and BaseUnitAbbreviation are derived when reading.
	PartSKU              string
	PartName             string
	BaseUnitAbbreviation string
	// UnitID is the packaging unit EnteredQuantity is typed in; nil for the
	// base unit. UnitAbbreviation is derived.
	UnitID           *string
	UnitAbbreviation string
	// EnteredQuantity is typed in UnitID; Quantity is the base-unit amount.
	EnteredQuantity string
	Quantity        string
	// AverageCost is the stock's average cost per base unit (VND, up to four
	// decimals) at the moment the stock left; nil when unknown. A write-off
	// values its lines at it, and cancelling either document puts the stock
	// back at it.
	AverageCost *string
	// CreditUnitCost is the credit of a return to supplier, VND per entered
	// unit; unused by write-offs.
	CreditUnitCost int64
}

// Amount is the line's credit, whole VND (a return to supplier).
func (l *AdjustmentLine) Amount() int64 { return LineAmount(l.EnteredQuantity, l.CreditUnitCost) }

// Value is the line's stock value at average cost, whole VND (a write-off);
// zero when the cost is unknown.
func (l *AdjustmentLine) Value() int64 {
	if l.AverageCost == nil {
		return 0
	}

	n, _ := ValueDong(l.Quantity, *l.AverageCost)

	return n
}

// WriteOff is a numbered document that reduces stock at average cost
// (FR-INV-09).
type WriteOff struct {
	ID       string
	GarageID string
	Number   string
	Status   AdjustmentStatus
	Reason   WriteOffReason
	Notes    string
	Lines    []AdjustmentLine
	// TotalValue is the sum of the lines' values; the repository fills it
	// for list pages, which carry no lines.
	TotalValue    int64
	CancelledAt   *time.Time
	CreatedBy     *string
	UpdatedBy     *string
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Total sums the values of the lines.
func (w *WriteOff) Total() int64 {
	var total int64
	for i := range w.Lines {
		total += w.Lines[i].Value()
	}

	return total
}

// SupplierReturn is a numbered document that reduces stock and records the
// supplier's credit (FR-INV-10).
type SupplierReturn struct {
	ID       string
	GarageID string
	Number   string
	Status   AdjustmentStatus
	// SupplierName and GoodsReceiptNumber are derived when reading.
	SupplierID         string
	SupplierName       string
	GoodsReceiptID     *string
	GoodsReceiptNumber string
	CreditNoteNumber   string
	Notes              string
	Lines              []AdjustmentLine
	// CreditAmount is the sum of the lines' credits; the repository fills it
	// for list pages, which carry no lines.
	CreditAmount  int64
	CancelledAt   *time.Time
	CreatedBy     *string
	UpdatedBy     *string
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Total sums the credits of the lines.
func (s *SupplierReturn) Total() int64 {
	var total int64
	for i := range s.Lines {
		total += s.Lines[i].Amount()
	}

	return total
}

// RepairOrderPartReturn records unused parts of a repair order coming back
// to stock (FR-INV-11); it is immutable.
type RepairOrderPartReturn struct {
	ID                string
	GarageID          string
	RepairOrderID     string
	RepairOrderNumber string
	StockIssueID      string
	StockIssueLineID  string
	// RepairOrderLineID is the order's part line the parts came from.
	RepairOrderLineID string
	PartID            string
	// PartSKU, PartName and BaseUnitAbbreviation are derived when reading.
	PartSKU              string
	PartName             string
	BaseUnitAbbreviation string
	UnitID               *string
	UnitAbbreviation     string
	EnteredQuantity      string
	Quantity             string
	Reason               string
	// UnitCost is the cost per base unit the parts are restored at (the
	// stock issue line's snapshot); nil when unknown.
	UnitCost      *string
	CreatedBy     *string
	CreatedByName string
	CreatedAt     time.Time
}

// MaxReasonLength bounds the reason of a repair order part return and
// MaxNameLength the name of a stock count.
const (
	MaxReasonLength = 500
	MaxNameLength   = 255
)

// Errors of the Release 7.1 adjustments.
var (
	ErrWriteOffNotFound               = errors.New("write-off not found")
	ErrWriteOffAlreadyCancelled       = errors.New("the write-off is already cancelled")
	ErrInvalidWriteOffReason          = errors.New("the write-off reason is invalid")
	ErrSupplierReturnNotFound         = errors.New("supplier return not found")
	ErrSupplierReturnAlreadyCancelled = errors.New("the supplier return is already cancelled")
	ErrGoodsReceiptNotCompleted       = errors.New("the goods receipt is not completed")
	ErrReceiptSupplierMismatch        = errors.New("the goods receipt has no line from this supplier")
	// ErrNoParts is returned when adding lines to a count without naming any
	// part or category.
	ErrNoParts = errors.New("name at least one part or a category")
	// ErrStockCountEmpty is returned when completing a count with no counted
	// line.
	ErrStockCountEmpty = errors.New("the stock count has no counted lines")
	// ErrNameTooLong is returned for a count name beyond MaxNameLength.
	ErrNameTooLong       = errors.New("the name is too long")
	ErrInsufficientStock = errors.New("part stock is lower than the quantity requested")
	ErrReasonRequired    = errors.New("a reason is required")
	ErrReasonTooLong     = errors.New("the reason is too long")
)

// NormalizeReason trims the reason of a part return and checks it.
func NormalizeReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrReasonRequired
	}

	if len([]rune(s)) > MaxReasonLength {
		return "", ErrReasonTooLong
	}

	return s, nil
}

// CreditPerEnteredUnit converts a cost per base unit (a decimal string) to
// VND per entered unit, baseCost times quantity over entered, rounded half up;
// zero when an operand cannot be used.
func CreditPerEnteredUnit(baseCost, quantity, entered string) int64 {
	c, ok1 := new(big.Rat).SetString(baseCost)
	q, ok2 := new(big.Rat).SetString(quantity)
	e, ok3 := new(big.Rat).SetString(entered)

	if !ok1 || !ok2 || !ok3 || e.Sign() <= 0 || c.Sign() <= 0 {
		return 0
	}

	return roundHalfUp(new(big.Rat).Quo(new(big.Rat).Mul(c, q), e))
}
