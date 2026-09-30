// Package domain contains pure business types for the Inventory module
// (HLD module #7). See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Field length limits shared by the module's use cases.
const (
	MaxSKULen  = 50
	MaxNameLen = 255
	MaxUnitLen = 20
)

// Item is one part code a garage stocks (FR-INV-01..03).
type Item struct {
	ID               string
	GarageID         string
	SKU              string
	Name             string
	Unit             string
	ReorderThreshold int
	// QuantityOnHand is derived from stock_balance when reading, never
	// persisted on the item itself (FR-INV-03). Decimal string.
	QuantityOnHand string
	CreatedBy      *string
	UpdatedBy      *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LowStock reports whether the item is at or below its reorder threshold
// (FR-INV-04's display half; the notification itself is Release 2).
func (i *Item) LowStock() bool {
	qty, err := strconv.ParseFloat(i.QuantityOnHand, 64)

	return err == nil && qty <= float64(i.ReorderThreshold)
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
	ItemID   string
	// ItemSKU/ItemName are derived when reading, never persisted.
	ItemSKU  string
	ItemName string
	Type     TransactionType
	// Decimal string, always positive; Type gives the direction.
	Quantity string
	// UnitCost is set for a purchase receipt, nil for a repair-order issue.
	UnitCost  *int64
	RefType   RefType
	RefID     *string
	CreatedBy *string
	CreatedAt time.Time
}

// Sentinel errors for business-rule violations in the Inventory module.
var (
	ErrItemNotFound = errors.New("item not found")

	ErrSKURequired             = errors.New("SKU is required")
	ErrNameRequired            = errors.New("name is required")
	ErrInvalidQuantity         = errors.New("quantity must be a positive number with at most 2 decimal places")
	ErrInvalidReorderThreshold = errors.New("reorder threshold must not be negative")
	ErrFieldTooLong            = errors.New("field is too long")

	ErrSKUTaken = errors.New("another item already uses this SKU")

	// ErrAlreadyIssued is returned when a repair order's stock issue for
	// an item was already recorded (the unique index on
	// (garage_id, item_id, ref_id) where ref_type = 'repair_order'),
	// guarding a redelivered repair_order.closed event on top of the
	// generic processed_events check.
	ErrAlreadyIssued = errors.New("stock for this repair order and item was already issued")
)

// CheckLength returns ErrFieldTooLong when value has more than maxLen
// characters.
func CheckLength(value string, maxLen int) error {
	if len([]rune(value)) > maxLen {
		return ErrFieldTooLong
	}

	return nil
}

var quantityPattern = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)

// ParseQuantity validates a stock quantity: a positive decimal with at
// most 2 decimal places. Unlike Quote & Pricing's ParseQuantity, the
// result is not scaled -- Inventory never multiplies a quantity by a
// price in Go, only in SQL (stock_balance arithmetic), so the trimmed
// string itself is enough.
func ParseQuantity(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !quantityPattern.MatchString(s) {
		return "", ErrInvalidQuantity
	}

	qty, err := strconv.ParseFloat(s, 64)
	if err != nil || qty <= 0 {
		return "", ErrInvalidQuantity
	}

	return s, nil
}
