package domain

import (
	"errors"
	"math/big"
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
	// ShortQuantity is the base-unit amount the stock cannot cover yet,
	// "0" when covered (Release 7.3); derived when reading a draft.
	ShortQuantity string
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
	// IsShort is true while a draft cannot be covered by the stock on hand
	// (Release 7.3); the repair order is then waiting on parts.
	IsShort bool
}

// DraftLine is one part line of a draft stock issue, as the reservation order
// sees it.
type DraftLine struct {
	IssueID       string
	RepairOrderID string
	// IssueIsShort is the mark stored on the issue.
	IssueIsShort bool
	// Affected is true when the issue holds a part whose stock just changed,
	// so its mark may have to change; the other issues are only listed to
	// serve their lines in order.
	Affected bool
	LineID   string
	PartID   string
	Quantity string
}

// Shortages serves the draft lines, given in reservation order (the issue
// created first first), against what is on hand per part and returns the
// quantity of each line the stock cannot cover, keyed by line id; lines that
// are covered are absent. A part is served from its own stock only: what an
// earlier line reserves is not left for a later one.
func Shortages(lines []DraftLine, onHand map[string]string) map[string]string {
	left := make(map[string]*big.Rat, len(onHand))
	short := map[string]string{}

	for _, l := range lines {
		stock, ok := left[l.PartID]
		if !ok {
			stock, _ = new(big.Rat).SetString(onHand[l.PartID])
			if stock == nil || stock.Sign() < 0 {
				stock = new(big.Rat)
			}

			left[l.PartID] = stock
		}

		need, ok := new(big.Rat).SetString(l.Quantity)
		if !ok || need.Sign() <= 0 {
			continue
		}

		if stock.Cmp(need) >= 0 {
			stock.Sub(stock, need)

			continue
		}

		short[l.LineID] = trimDecimal(new(big.Rat).Sub(need, stock).FloatString(3))
		stock.SetInt64(0)
	}

	return short
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
