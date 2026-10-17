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
)
