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
	// RefRepairOrder is an automatic deduction when a repair order with
	// part lines closes (FR-INV-02).
	RefRepairOrder RefType = "repair_order"
)

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
	// UnitCost is the cost of one base unit (VND, up to four decimals) of a
	// purchase receipt, nil for a repair-order issue or an unrecorded cost.
	UnitCost  *string
	RefType   RefType
	RefID     *string
	CreatedBy *string
	CreatedAt time.Time
}

// Sentinel errors for business-rule violations in the Inventory module.
var (
	ErrPartNotFound    = errors.New("part not found")
	ErrPartInactive    = errors.New("the part is not active")
	ErrInvalidQuantity = errors.New("quantity must be a positive number with at most 3 decimal places")
	ErrInvalidCost     = errors.New("unit cost must not be negative")

	// ErrAlreadyIssued is returned when a repair order's stock issue for
	// a part was already recorded (the unique index on
	// (garage_id, item_id, ref_id) where ref_type = 'repair_order'),
	// guarding a redelivered repair_order.closed event on top of the
	// generic processed_events check.
	ErrAlreadyIssued = errors.New("stock for this repair order and part was already issued")
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
