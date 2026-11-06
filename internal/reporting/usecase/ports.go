package usecase

import (
	"context"
	"errors"
	"time"
)

// TechnicianWork is what one technician completed in a period.
type TechnicianWork struct {
	TechnicianID             string
	CompletedLines           int
	RepairOrders             int
	AverageTurnaroundMinutes int64
}

// WorkReader reads the completed lines per technician; implemented by an
// adapter over the Repair Order module in cmd/server. from (inclusive) and to
// (exclusive) are calendar dates of timezone.
type WorkReader interface {
	CompletedWork(ctx context.Context, garageID, timezone string, from, to time.Time) ([]TechnicianWork, error)
}

// ProfitLine is one line of a repair order as the profit report sees it.
type ProfitLine struct {
	RepairOrderID     string
	RepairOrderNumber string
	LineID            string
	// JobID is empty for a line of no job.
	JobID string
	// Revenue is the line's total; a hidden consumable has none.
	Revenue int64
	// Consumes is true for a part line.
	Consumes      bool
	TechnicianIDs []string
}

// ErrOrderNotFound is returned by an OrderReader for a repair order that does
// not exist in the garage.
var ErrOrderNotFound = errors.New("repair order not found")

// OrderReader reads the lines of repair orders; implemented by an adapter over
// the Repair Order module in cmd/server.
type OrderReader interface {
	// ClosedLines returns every line of the repair orders closed in [from, to)
	// (calendar dates of timezone).
	ClosedLines(ctx context.Context, garageID, timezone string, from, to time.Time) ([]ProfitLine, error)
	// OrderLines returns every line of one repair order, or ErrOrderNotFound.
	OrderLines(ctx context.Context, garageID, repairOrderID string) ([]ProfitLine, error)
}

// IssueCost is the cost of the parts issued to one repair order.
type IssueCost struct {
	// HasIssue is true when the order has a stock issue; Posted when it is posted.
	HasIssue, Posted bool
	// ByLine is the cost per repair order line, whole VND, from the posted issue.
	ByLine map[string]int64
}

// CostReader reads the cost snapshots of the stock issues; implemented by an
// adapter over the Inventory module in cmd/server. An order with no stock issue
// has no entry.
type CostReader interface {
	IssueCosts(ctx context.Context, garageID string, repairOrderIDs []string) (map[string]IssueCost, error)
}

// NameReader names the technicians and the jobs of a report; implemented by an
// adapter over Identity and Catalogue in cmd/server. Unknown ids are absent.
type NameReader interface {
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	JobNames(ctx context.Context, garageID string, ids []string) (map[string]string, error)
}
