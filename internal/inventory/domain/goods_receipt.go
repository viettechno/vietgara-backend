package domain

import (
	"errors"
	"math/big"
	"strings"
	"time"
)

// GoodsReceiptStatus is the lifecycle of a goods receipt (FR-INV-06): saved
// as a draft, completed (which posts stock and cost), or cancelled (which
// reverses a completed one).
type GoodsReceiptStatus string

// Valid GoodsReceiptStatus values.
const (
	GoodsReceiptDraft     GoodsReceiptStatus = "draft"
	GoodsReceiptCompleted GoodsReceiptStatus = "completed"
	GoodsReceiptCancelled GoodsReceiptStatus = "cancelled"
)

// IsValid reports whether s is one of the defined statuses.
func (s GoodsReceiptStatus) IsValid() bool {
	return s == GoodsReceiptDraft || s == GoodsReceiptCompleted || s == GoodsReceiptCancelled
}

// GoodsReceiptLine is one part on a goods receipt, with its supplier.
type GoodsReceiptLine struct {
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
	// amount. Both are decimal strings.
	EnteredQuantity string
	Quantity        string
	// SupplierID may be nil while the receipt is a draft; completion needs
	// one on every line (ErrSupplierRequired). SupplierName is derived.
	SupplierID   *string
	SupplierName string
	// UnitCost is VND per entered unit as typed; 0 means "not recorded".
	UnitCost int64
}

// Amount is the line's running total, whole VND.
func (l *GoodsReceiptLine) Amount() int64 {
	return LineAmount(l.EnteredQuantity, l.UnitCost)
}

// GoodsReceipt is a numbered multi-line document recording a delivery
// (FR-INV-06).
type GoodsReceipt struct {
	ID       string
	GarageID string
	// Number is rendered from the garage's goods_receipt numbering
	// template (FR-TEN-05).
	Number string
	Status GoodsReceiptStatus
	// DefaultSupplierID is inherited by new lines; DefaultSupplierName is
	// derived.
	DefaultSupplierID   *string
	DefaultSupplierName string
	// ReceiptDate is "YYYY-MM-DD" in the garage's timezone.
	ReceiptDate        string
	DeliveryNoteNumber string
	Notes              string
	Lines              []GoodsReceiptLine
	// TotalAmount is the sum of the lines' amounts; the repository fills it
	// for list pages, which carry no lines.
	TotalAmount int64
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedBy   *string
	UpdatedBy   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Total sums the amounts of the lines.
func (r *GoodsReceipt) Total() int64 {
	var total int64
	for i := range r.Lines {
		total += r.Lines[i].Amount()
	}

	return total
}

// Errors of the goods receipt document.
var (
	ErrGoodsReceiptNotFound = errors.New("goods receipt not found")
	// ErrGoodsReceiptNotDraft is returned when a change or completion needs
	// a draft.
	ErrGoodsReceiptNotDraft = errors.New("only a draft goods receipt can be changed or completed")
	// ErrGoodsReceiptAlreadyCancelled is returned for cancelling twice.
	ErrGoodsReceiptAlreadyCancelled = errors.New("the goods receipt is already cancelled")
	// ErrSupplierRequired is returned when completing a receipt that has a
	// line without a supplier (FR-INV-06).
	ErrSupplierRequired = errors.New("every line needs a supplier before the receipt can be completed")
	// ErrSupplierNotFound is returned for a supplier that does not exist in
	// the garage.
	ErrSupplierNotFound = errors.New("supplier not found")
	// ErrNoLines is returned when completing a receipt without lines.
	ErrNoLines = errors.New("the goods receipt has no lines")
	// ErrInvalidReceiptDate is returned for a date that is not YYYY-MM-DD.
	ErrInvalidReceiptDate = errors.New("receipt date must be a date in the form YYYY-MM-DD")
	// ErrInsufficientStockToReverse is returned when cancelling a completed
	// receipt would take a part below zero because its stock was used.
	ErrInsufficientStockToReverse = errors.New("part stock is lower than the receipt quantity, so the receipt cannot be reversed")
	// ErrDuplicateSupplier is returned when a part lists a supplier twice.
	ErrDuplicateSupplier = errors.New("a supplier can be listed once per part")
	// ErrTooManyDefaults is returned for more than one default supplier.
	ErrTooManyDefaults = errors.New("a part has at most one default supplier")
	// ErrInvalidReport is returned for a bad date or range of a report.
	ErrInvalidReport = errors.New("dates must be in the form YYYY-MM-DD and from must be before to")
)

// ParseDate validates a "YYYY-MM-DD" date.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, ErrInvalidReceiptDate
	}

	return t, nil
}

// LineAmount is entered times unitCost, rounded half up to whole VND.
// entered is a decimal string; an unparsable one counts as zero.
func LineAmount(entered string, unitCost int64) int64 {
	e, ok := new(big.Rat).SetString(entered)
	if !ok || unitCost <= 0 {
		return 0
	}

	return roundHalfUp(new(big.Rat).Mul(e, big.NewRat(unitCost, 1)))
}

// StockValue is quantity times averageCost, rounded to four decimals; both
// are decimal strings and an unparsable one counts as zero.
func StockValue(quantity, averageCost string) string {
	q, ok1 := new(big.Rat).SetString(quantity)
	a, ok2 := new(big.Rat).SetString(averageCost)

	if !ok1 || !ok2 {
		return "0"
	}

	return trimDecimal(new(big.Rat).Mul(q, a).FloatString(4))
}

// ValueDong is quantity times cost rounded once, half up, to whole VND; both
// are decimal strings and ok is false when either cannot be parsed.
func ValueDong(quantity, cost string) (n int64, ok bool) {
	q, ok1 := new(big.Rat).SetString(quantity)
	c, ok2 := new(big.Rat).SetString(cost)

	if !ok1 || !ok2 {
		return 0, false
	}

	return roundHalfUp(new(big.Rat).Mul(q, c)), true
}

// WholeDong rounds a cost with up to four decimals half up to whole VND; ok
// is false when cost cannot be parsed.
func WholeDong(cost string) (n int64, ok bool) {
	r, ok := new(big.Rat).SetString(cost)
	if !ok {
		return 0, false
	}

	return roundHalfUp(r), true
}

func roundHalfUp(r *big.Rat) int64 {
	neg := r.Sign() < 0
	abs := new(big.Rat).Abs(r)
	abs.Add(abs, big.NewRat(1, 2))
	n := new(big.Int).Quo(abs.Num(), abs.Denom()).Int64()

	if neg {
		return -n
	}

	return n
}

func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}

	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// PartSupplier is one usual supplier of a part (FR-INV-19).
type PartSupplier struct {
	SupplierID   string
	SupplierName string
	// SupplierSKU is the supplier's own code for the part.
	SupplierSKU string
	// LastUnitCost is the last cost paid per base unit (VND, up to four
	// decimals); nil when never paid. Hidden without inventory.cost.read.
	LastUnitCost *string
	IsDefault    bool
}

// ValidatePartSuppliers checks a replacement list: each supplier once, at
// most one default.
func ValidatePartSuppliers(list []PartSupplier) error {
	seen := map[string]bool{}
	defaults := 0

	for _, s := range list {
		if seen[s.SupplierID] {
			return ErrDuplicateSupplier
		}

		seen[s.SupplierID] = true

		if s.IsDefault {
			defaults++
		}
	}

	if defaults > 1 {
		return ErrTooManyDefaults
	}

	return nil
}

// PurchaseLine is one line of a completed goods receipt in the purchases
// report (FR-INV-19).
type PurchaseLine struct {
	GoodsReceiptID     string
	GoodsReceiptNumber string
	// ReceiptDate is "YYYY-MM-DD".
	ReceiptDate          string
	SupplierID           string
	SupplierName         string
	PartID               string
	PartSKU              string
	PartName             string
	Quantity             string
	BaseUnitAbbreviation string
	// UnitCost is VND per base unit; Amount is Quantity times UnitCost,
	// whole VND.
	UnitCost int64
	Amount   int64
}

// StockValuationLine is one part's quantity and value at a date (FR-INV-07).
type StockValuationLine struct {
	PartID               string
	PartSKU              string
	PartName             string
	CategoryID           string
	CategoryName         string
	QuantityOnHand       string
	BaseUnitAbbreviation string
	AverageCost          int64
	Value                int64
}

// StockValuationCategory is one category's total value.
type StockValuationCategory struct {
	CategoryID   string
	CategoryName string
	Value        int64
}

// Subtract returns a minus b for decimal strings, trimmed; "0" when either
// cannot be parsed.
func Subtract(a, b string) string {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)

	if !ok1 || !ok2 {
		return "0"
	}

	return trimDecimal(new(big.Rat).Sub(x, y).FloatString(3))
}

// Add returns a plus b for decimal strings, trimmed; an empty or unparsable
// operand counts as zero.
func Add(a, b string) string {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)

	if !ok1 {
		x = new(big.Rat)
	}

	if !ok2 {
		y = new(big.Rat)
	}

	return trimDecimal(new(big.Rat).Add(x, y).FloatString(3))
}

// Less reports whether a is smaller than b; decimal strings, and an
// unparsable operand counts as zero.
func Less(a, b string) bool {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)

	if !ok1 {
		x = new(big.Rat)
	}

	if !ok2 {
		y = new(big.Rat)
	}

	return x.Cmp(y) < 0
}

// IsZero reports whether a decimal string is zero; an unparsable one counts
// as zero.
func IsZero(a string) bool {
	x, ok := new(big.Rat).SetString(a)

	return !ok || x.Sign() == 0
}
