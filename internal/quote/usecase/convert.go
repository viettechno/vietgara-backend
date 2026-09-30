package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

// ConvertQuoteResult is the outcome of QuoteUseCase.Convert.
type ConvertQuoteResult struct {
	Quote         *domain.Quote
	RepairOrderID string
}

// Convert converts an approved quote into a draft repair order (FR-QUO-04),
// preserving only the approved lines. Both the repair order's creation and
// the quote's own transition to "converted" run in the caller's
// garage-scoped transaction (middleware.Garage), so they commit or roll
// back together -- a failed repair-order creation never leaves a quote
// stranded as "approved but silently unconvertible". Counts against the
// plan's daily transaction limit (FR-LIC-02), separately from the quote's
// own creation.
func (uc *QuoteUseCase) Convert(ctx context.Context, garageID, quoteID, actorID string) (ConvertQuoteResult, error) {
	q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
	if err != nil {
		return ConvertQuoteResult{}, toAppError("convert quote", err)
	}

	if q.Status != domain.StatusApproved {
		return ConvertQuoteResult{}, toAppError("convert quote", domain.ErrNotApproved)
	}

	approved := approvedLines(q.Lines)

	garage, err := uc.garageInfo(ctx, garageID)
	if err != nil {
		return ConvertQuoteResult{}, err
	}

	if err := uc.Limits.ConsumeTransaction(ctx, garage.OwnerAccountID); err != nil {
		return ConvertQuoteResult{}, err
	}

	number, err := uc.nextRepairOrderNumber(ctx, garageID, garage)
	if err != nil {
		return ConvertQuoteResult{}, toAppError("convert quote", err)
	}

	repairOrderID, err := uc.RepairOrders.Create(
		ctx, garageID, q.ID, q.VehicleID, q.CustomerName, q.VehicleLicensePlate, number, actorID, approved,
	)
	if err != nil {
		return ConvertQuoteResult{}, toAppError("convert quote", err)
	}

	now := uc.Now()
	q.Status = domain.StatusConverted
	q.ConvertedAt = &now
	q.UpdatedBy = actorRef(actorID)

	if err := uc.Quotes.Save(ctx, q); err != nil {
		return ConvertQuoteResult{}, toAppError("convert quote", err)
	}

	return ConvertQuoteResult{Quote: q, RepairOrderID: repairOrderID}, nil
}

// approvedLines returns the customer-approved lines only, renumbered from
// 1 so the repair order's own line order stays gap-free.
func approvedLines(lines []domain.Line) []domain.Line {
	out := make([]domain.Line, 0, len(lines))

	for _, line := range lines {
		if line.Decision == domain.DecisionApproved {
			line.Position = len(out) + 1
			out = append(out, line)
		}
	}

	return out
}
