package usecase

import (
	"context"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

// LineDecisionInput is one line's decision (FR-QUO-03): id-matched against
// the quote's current lines, every line must be covered exactly once.
type LineDecisionInput struct {
	LineID   string
	Approved bool
}

// RecordQuoteDecision records the customer's decision on behalf of the
// customer (FR-QUO-03), for a MANUAL-mode quote or as a staff override of
// a LINK-mode one (e.g. the customer decided by phone). Runs in the
// garage-scoped transaction middleware.Garage already opened.
func (uc *QuoteUseCase) RecordQuoteDecision(ctx context.Context, garageID, quoteID, actorID string, inputs []LineDecisionInput) (*domain.Quote, error) {
	q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
	if err != nil {
		return nil, toAppError("record quote decision", err)
	}

	if err := uc.applyDecisions(q, inputs); err != nil {
		return nil, toAppError("record quote decision", err)
	}

	q.UpdatedBy = actorRef(actorID)

	if err := uc.Quotes.Save(ctx, q); err != nil {
		return nil, toAppError("record quote decision", err)
	}

	if err := uc.publishDecisionEvent(ctx, q); err != nil {
		return nil, toAppError("record quote decision", err)
	}

	return q, nil
}

// GetPublicQuote returns the quote an approval link's token names, for the
// customer to review before deciding. The public flow names no garage, so
// this opens its own transaction once the token resolves one (see
// Repository.FindByPublicToken); every other RPC gets this from
// middleware.Garage instead.
func (uc *QuoteUseCase) GetPublicQuote(ctx context.Context, token string) (*domain.Quote, error) {
	hash := domain.HashPublicToken(token)

	garageID, quoteID, err := uc.Quotes.FindByPublicToken(ctx, hash)
	if err != nil {
		return nil, toAppError("get public quote", err)
	}

	var result *domain.Quote

	err = uc.Tx.WithTransaction(ctx, platformdb.GarageScope(garageID), func(ctx context.Context) error {
		q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
		if err != nil {
			return err
		}

		result = q

		return nil
	})
	if err != nil {
		return nil, toAppError("get public quote", err)
	}

	return result, nil
}

// DecidePublicQuote records the customer's own decision through the public
// approval link (FR-QUO-02/03), with no login: the token is the
// authorization. See GetPublicQuote for why this opens its own
// transaction.
func (uc *QuoteUseCase) DecidePublicQuote(ctx context.Context, token string, inputs []LineDecisionInput) (*domain.Quote, error) {
	hash := domain.HashPublicToken(token)

	garageID, quoteID, err := uc.Quotes.FindByPublicToken(ctx, hash)
	if err != nil {
		return nil, toAppError("decide quote", err)
	}

	var result *domain.Quote

	err = uc.Tx.WithTransaction(ctx, platformdb.GarageScope(garageID), func(ctx context.Context) error {
		q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
		if err != nil {
			return err
		}

		if q.ApprovalMode != domain.ApprovalModeLink || q.PublicTokenHash == nil || *q.PublicTokenHash != hash {
			return domain.ErrPublicTokenInvalid
		}

		if err := uc.applyDecisions(q, inputs); err != nil {
			return err
		}

		if err := uc.Quotes.Save(ctx, q); err != nil {
			return err
		}

		result = q

		return uc.publishDecisionEvent(ctx, q)
	})
	if err != nil {
		return nil, toAppError("decide quote", err)
	}

	return result, nil
}

// applyDecisions requires inputs to cover exactly q's current lines, sets
// each line's decision, and derives the quote's overall status: approved
// if at least one line was approved, otherwise rejected (FR-QUO-03/04 --
// only approved lines convert into the repair order). Clears the public
// token either way, so a decided quote's link stops working.
func (uc *QuoteUseCase) applyDecisions(q *domain.Quote, inputs []LineDecisionInput) error {
	if !q.Status.Decidable() {
		return domain.ErrNotEditable
	}

	if len(inputs) != len(q.Lines) {
		return domain.ErrDecisionMismatch
	}

	approvals := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		approvals[in.LineID] = in.Approved
	}

	now := uc.Now()
	anyApproved := false

	for i := range q.Lines {
		approved, ok := approvals[q.Lines[i].ID]
		if !ok {
			return domain.ErrDecisionMismatch
		}

		if approved {
			q.Lines[i].Decision = domain.DecisionApproved
			anyApproved = true
		} else {
			q.Lines[i].Decision = domain.DecisionRejected
		}

		q.Lines[i].DecidedAt = &now
	}

	q.Status = domain.StatusRejected
	if anyApproved {
		q.Status = domain.StatusApproved
	}

	q.DecidedAt = &now
	q.PublicTokenHash, q.PublicTokenExpiresAt = nil, nil

	return nil
}

func (uc *QuoteUseCase) publishDecisionEvent(ctx context.Context, q *domain.Quote) error {
	event := EventQuoteRejected
	if q.Status == domain.StatusApproved {
		event = EventQuoteApproved
	}

	return uc.Events.Publish(ctx, event, q.GarageID, "", map[string]any{
		"quote_id": q.ID, keyGarageID: q.GarageID, "customer_id": q.CustomerID, "status": string(q.Status),
	})
}
