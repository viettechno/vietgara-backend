// Package domain contains pure business types for the Inventory module
// (HLD module #7): stock on hand, stock movements and their rules. The part
// itself (SKU, name, units, price, category, images) belongs to the
// Catalogue module; Inventory refers to it by id only. See
// Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// StockLevel is the stock position of one part: the Catalogue's description
// of the part joined with the quantity Inventory holds (FR-INV-03). It is
// derived when reading and never persisted as such.
type StockLevel struct {
	PartID               string
	SKU                  string
	Name                 string
	BaseUnitName         string
	BaseUnitAbbreviation string
	CategoryName         string
	Brand                string
	IsActive             bool
	ThumbnailURL         string
	// QuantityOnHand and ReorderThreshold are decimal strings in the
	// part's base unit.
	QuantityOnHand   string
	ReorderThreshold string
	// QuantityReserved is held by the part's draft stock issues and
	// QuantityAvailable is QuantityOnHand minus it (FR-INV-20); both are
	// decimal strings in the base unit.
	QuantityReserved  string
	QuantityAvailable string
	// AverageCost is the weighted-average cost per base unit, VND with up to
	// four decimals (FR-INV-07); StockValue is AverageCost times
	// QuantityOnHand. Both are cleared for a caller without
	// inventory.cost.read.
	AverageCost *string
	StockValue  *string
	// LowStock is true when the quantity is at or below the threshold
	// (FR-INV-04's display half).
	LowStock bool
}

// IsLow reports whether quantity is at or below threshold; both are decimal
// strings, and an unparsable value is never low.
func IsLow(quantity, threshold string) bool {
	q, ok := new(big.Rat).SetString(quantity)
	if !ok {
		return false
	}

	t, ok := new(big.Rat).SetString(threshold)
	if !ok {
		return false
	}

	return q.Cmp(t) <= 0
}

// TransactionType is a stock movement's direction: it either adds to stock or removes from it.
type TransactionType string

// Valid TransactionType values.
const (
	TransactionIn  TransactionType = "in"
	TransactionOut TransactionType = "out"
)

// RefType names what caused a stock movement.
type RefType string

// Valid RefType values.
const (
	// RefPurchase is a manual stock receipt from a supplier (FR-INV-01).
	RefPurchase RefType = "purchase"
	// RefRepairOrder is the pre-7.0 automatic deduction when a repair order
	// with part lines closed (FR-INV-02); superseded by RefStockIssue.
	RefRepairOrder RefType = "repair_order"
	// RefGoodsReceipt is a completed goods receipt, or the reversal of a
	// cancelled one (FR-INV-06); RefID is the receipt's id.
	RefGoodsReceipt RefType = "goods_receipt"
	// RefStockIssue is a posted stock issue (FR-INV-20); RefID is the issue's
	// id.
	RefStockIssue RefType = "stock_issue"
	// RefStockCount is the adjustment a completed stock count posted, or its
	// reversal (FR-INV-12); RefID is the count's id.
	RefStockCount RefType = "stock_count"
	// RefWriteOff is a write-off, or the reversal of a cancelled one
	// (FR-INV-09).
	RefWriteOff RefType = "write_off"
	// RefSupplierReturn is a return to supplier, or the reversal of a
	// cancelled one (FR-INV-10).
	RefSupplierReturn RefType = "supplier_return"
	// RefRepairOrderReturn is unused parts returned from a repair order
	// (FR-INV-11); RefID is the part return's id.
	RefRepairOrderReturn RefType = "repair_order_return"
)

// IsValid reports whether t is one of the defined reference types.
func (t RefType) IsValid() bool {
	switch t {
	case RefPurchase, RefRepairOrder, RefGoodsReceipt, RefStockIssue, RefStockCount, RefWriteOff, RefSupplierReturn,
		RefRepairOrderReturn:
		return true
	default:
		return false
	}
}

// IsAdjustment reports whether t is a Release 7.1 adjustment, whose movements
// are valued at the average cost of the moment and so never move it.
func (t RefType) IsAdjustment() bool {
	switch t {
	case RefStockCount, RefWriteOff, RefSupplierReturn:
		return true
	case RefPurchase, RefRepairOrder, RefGoodsReceipt, RefStockIssue, RefRepairOrderReturn:
		return false
	default:
		return false
	}
}

// StockTransaction is one recorded stock movement.
type StockTransaction struct {
	ID       string
	GarageID string
	PartID   string
	// PartSKU/PartName are derived when reading, never persisted.
	PartSKU  string
	PartName string
	// BaseUnitAbbreviation is the part's base unit, also derived.
	BaseUnitAbbreviation string
	Type                 TransactionType
	// Quantity is the base-unit amount, a decimal string, always positive;
	// Type gives the direction.
	Quantity string
	// UnitID, EnteredQuantity and UnitAbbreviation describe the packaging
	// unit the user typed the quantity in; nil/empty for the base unit.
	UnitID           *string
	EnteredQuantity  *string
	UnitAbbreviation string
	// UnitCost is the cost of one base unit (VND, up to four decimals): the
	// line cost of a goods receipt, or the average-cost snapshot of an issue.
	// Nil when no cost was recorded; a stock issue is stamped with the
	// average cost at the time it is recorded.
	UnitCost *string
	RefType  RefType
	RefID    *string
	// RefNumber is the number of the document behind the movement and
	// Reason why an adjustment happened; QuantityAfter is the part's quantity
	// on hand right after the movement. All three are derived when reading.
	RefNumber     string
	Reason        string
	QuantityAfter string
	// CreatedByName names CreatedBy, derived when reading.
	CreatedByName string
	// SupplierID is the supplier of the goods receipt line behind an IN
	// movement.
	SupplierID *string
	CreatedBy  *string
	CreatedAt  time.Time
}

// Sentinel errors for business-rule violations in the Inventory module.
var (
	ErrPartNotFound    = errors.New("part not found")
	ErrPartInactive    = errors.New("the part is not active")
	ErrInvalidQuantity = errors.New("quantity must be a positive number with at most 3 decimal places")
	ErrInvalidCost     = errors.New("unit cost must not be negative")
)

var quantityPattern = regexp.MustCompile(`^\d{1,10}(\.\d{1,3})?$`)

// ParseQuantity validates a base-unit stock quantity: a positive decimal with
// at most 3 decimal places.
func ParseQuantity(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !quantityPattern.MatchString(s) {
		return "", ErrInvalidQuantity
	}

	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() <= 0 {
		return "", ErrInvalidQuantity
	}

	return s, nil
}

// CostPerBaseUnit converts the cost typed per entered unit to the cost of one
// base unit: unitCost * entered / base, rounded half up to four decimals
// (FR-INV-01). entered and base are the typed and the converted quantity.
func CostPerBaseUnit(unitCost int64, entered, base string) (string, error) {
	if unitCost < 0 {
		return "", ErrInvalidCost
	}

	e, ok1 := new(big.Rat).SetString(entered)
	b, ok2 := new(big.Rat).SetString(base)

	if !ok1 || !ok2 || b.Sign() <= 0 {
		return "", ErrInvalidQuantity
	}

	cost := new(big.Rat).Mul(big.NewRat(unitCost, 1), e)
	cost.Quo(cost, b)

	return strings.TrimRight(strings.TrimRight(cost.FloatString(4), "0"), "."), nil
}
