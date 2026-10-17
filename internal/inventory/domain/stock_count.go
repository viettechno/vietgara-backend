package domain

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// StockCountStatus is the lifecycle of a stock count (FR-INV-12): a draft is
// counted line by line; completing it posts the adjustments and freezes it;
// cancelling a draft just closes it and cancelling a completed count posts
// the reversal.
type StockCountStatus string

// Valid StockCountStatus values.
const (
	StockCountDraft     StockCountStatus = "draft"
	StockCountCompleted StockCountStatus = "completed"
	StockCountCancelled StockCountStatus = "cancelled"
)

// IsValid reports whether s is one of the defined statuses.
func (s StockCountStatus) IsValid() bool {
	return s == StockCountDraft || s == StockCountCompleted || s == StockCountCancelled
}

// CountResult compares a line's counted quantity with the system quantity.
type CountResult string

// Valid CountResult values.
const (
	CountUncounted CountResult = "uncounted"
	CountMatched   CountResult = "matched"
	CountOver      CountResult = "over"
	CountShort     CountResult = "short"
)

// StockCountLine is one part of a stock count.
type StockCountLine struct {
	ID       string
	Position int
	PartID   string
	// PartSKU, PartName and BaseUnitAbbreviation are derived when reading.
	PartSKU              string
	PartName             string
	BaseUnitAbbreviation string
	// UnitID is the packaging unit EnteredCounted is typed in; nil for the
	// base unit. UnitAbbreviation is derived.
	UnitID           *string
	UnitAbbreviation string
	// EnteredCounted and Counted are decimal strings; both nil while the
	// line is uncounted. Counted is the base-unit amount.
	EnteredCounted *string
	Counted        *string
	// SystemQuantity is the stock on hand (base unit, decimal string, may be
	// negative) when the line was last counted.
	SystemQuantity string
	// AverageCost is the cost per base unit the difference is valued at (VND,
	// up to four decimals): stored when the count completes, the live
	// average while a draft; nil when unknown.
	AverageCost *string
	CountedAt   *time.Time
}

// Difference is counted minus system, a signed decimal string; ok is false
// while the line is uncounted.
func (l *StockCountLine) Difference() (diff string, ok bool) {
	if l.Counted == nil {
		return "", false
	}

	return Subtract(*l.Counted, l.SystemQuantity), true
}

// Result says whether the line matched, is over or short; CountUncounted
// before it is counted.
func (l *StockCountLine) Result() CountResult {
	diff, ok := l.Difference()
	if !ok {
		return CountUncounted
	}

	r, valid := new(big.Rat).SetString(diff)

	switch {
	case !valid || r.Sign() == 0:
		return CountMatched
	case r.Sign() > 0:
		return CountOver
	default:
		return CountShort
	}
}

// DifferenceValue is the difference valued at the line's average cost, whole
// VND and signed; nil while uncounted or without a known cost.
func (l *StockCountLine) DifferenceValue() *int64 {
	diff, ok := l.Difference()
	if !ok || l.AverageCost == nil {
		return nil
	}

	n, ok := ValueDong(diff, *l.AverageCost)
	if !ok {
		return nil
	}

	return &n
}

// StockCount is a numbered stock count document (FR-INV-12).
type StockCount struct {
	ID       string
	GarageID string
	// Number is rendered from the garage's stock_count numbering template.
	Number string
	Name   string
	Notes  string
	Status StockCountStatus
	Lines  []StockCountLine
	// Tally and TotalDifferenceValue summarise the lines; the use case fills
	// them from Lines, the repository for list pages, which carry no lines.
	Tally                Tally
	TotalDifferenceValue int64
	CompletedAt          *time.Time
	CompletedBy          *string
	// CompletedByName and CreatedByName are derived when reading.
	CompletedByName string
	CancelledAt     *time.Time
	CreatedByName   string
	CreatedBy       *string
	UpdatedBy       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Tally counts a stock count's lines by result.
type Tally struct {
	Uncounted int
	Matched   int
	Over      int
	Short     int
}

// Summarise fills Tally and TotalDifferenceValue from the lines.
func (c *StockCount) Summarise() {
	c.Tally, c.TotalDifferenceValue = Tally{}, 0

	for i := range c.Lines {
		l := &c.Lines[i]

		switch l.Result() {
		case CountUncounted:
			c.Tally.Uncounted++
		case CountMatched:
			c.Tally.Matched++
		case CountOver:
			c.Tally.Over++
		case CountShort:
			c.Tally.Short++
		}

		if v := l.DifferenceValue(); v != nil {
			c.TotalDifferenceValue += *v
		}
	}
}

// LineByID returns the index of the line with this id, or -1.
func (c *StockCount) LineByID(id string) int {
	for i := range c.Lines {
		if c.Lines[i].ID == id {
			return i
		}
	}

	return -1
}

// LineByPart returns the index of the line of this part, or -1.
func (c *StockCount) LineByPart(partID string) int {
	for i := range c.Lines {
		if c.Lines[i].PartID == partID {
			return i
		}
	}

	return -1
}

// Errors of the stock count document.
var (
	ErrStockCountNotFound     = errors.New("stock count not found")
	ErrStockCountLineNotFound = errors.New("stock count line not found")
	// ErrStockCountNotDraft is returned when a change, completion or merge
	// needs a draft.
	ErrStockCountNotDraft = errors.New("only a draft stock count can be changed or completed")
	// ErrStockCountAlreadyCancelled is returned for cancelling twice.
	ErrStockCountAlreadyCancelled = errors.New("the stock count is already cancelled")
	// ErrStockCountUncounted is returned when completing a count that still
	// has lines nobody counted.
	ErrStockCountUncounted = errors.New("some lines have not been counted yet")
	// ErrInvalidMerge is returned for fewer than two distinct counts.
	ErrInvalidMerge = errors.New("merging needs at least two different stock counts")
)

var countedPattern = regexp.MustCompile(`^\d{1,10}(\.\d{1,3})?$`)

// ParseCountedQuantity validates a counted quantity: zero or more, at most 3
// decimal places. Unlike ParseQuantity it accepts zero, a real count of none.
func ParseCountedQuantity(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !countedPattern.MatchString(s) {
		return "", ErrInvalidQuantity
	}

	return s, nil
}
