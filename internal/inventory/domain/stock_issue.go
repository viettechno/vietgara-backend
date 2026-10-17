package domain

import (
	"errors"
	"time"
)

// StockIssueStatus is the lifecycle of a stock issue (FR-INV-20): a draft
// follows its repair order and is the reservation; posting writes the stock
// movements; cancelling releases the reservation.
type StockIssueStatus string

// Valid StockIssueStatus values.
const (
	StockIssueDraft     StockIssueStatus = "draft"
	StockIssuePosted    StockIssueStatus = "posted"
	StockIssueCancelled StockIssueStatus = "cancelled"
)

// StockIssueLine is one part line of a stock issue, copied from the matching
// part line of the repair order.
type StockIssueLine struct {
	ID                string
	RepairOrderLineID string
	PartID            string
	// PartSKU, PartName and BaseUnitAbbreviation are derived when reading.
	PartSKU              string
	PartName             string
	BaseUnitAbbreviation string
	UnitID               *string
	UnitAbbreviation     string
	EnteredQuantity      string
	// Quantity is the base-unit amount.
	Quantity string
	// UnitCost is the average-cost snapshot per base unit taken on posting;
	// nil for a draft or an unknown cost.
	UnitCost *string
	// ReturnedQuantity is the base-unit amount already returned to stock
	// from this line (FR-INV-11), derived when reading.
	ReturnedQuantity string
}

// StockIssue is the numbered stock-out document of a repair order.
type StockIssue struct {
	ID                string
	GarageID          string
	Number            string
	RepairOrderID     string
	RepairOrderNumber string
	Status            StockIssueStatus
	Lines             []StockIssueLine
	PostedAt          *time.Time
	PostedBy          *string
	// PostedByName is derived when reading.
	PostedByName string
	CreatedBy    *string
	UpdatedBy    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Errors of the stock issue document.
var (
	ErrStockIssueNotFound = errors.New("stock issue not found")
	// ErrStockIssueNotDraft is returned when a change needs a draft.
	ErrStockIssueNotDraft = errors.New("only a draft stock issue can be changed")
	// ErrStockIssueNotPosted is returned when parts are returned from an
	// order whose stock issue is still a draft (nothing has left stock yet).
	ErrStockIssueNotPosted = errors.New("the stock issue is not posted, so nothing has left stock to return")
	// ErrStockIssueLineNotFound is returned for a repair order line the stock
	// issue has no line for.
	ErrStockIssueLineNotFound = errors.New("the repair order line is not on the stock issue")
	// ErrReturnExceedsIssued is returned when more would be returned than the
	// line issued.
	ErrReturnExceedsIssued = errors.New("the quantity returned from a line cannot exceed what was issued")
)
