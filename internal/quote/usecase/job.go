package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

// AddJobRequest is the input for QuoteUseCase.AddJob.
type AddJobRequest struct {
	GarageID string
	QuoteID  string
	JobID    string
	ActorID  string
}

// AddJob appends a catalogue job to a draft quote (FR-CAT-06): one labor line
// at the job's labor price, then one part line per suggested part at the
// part's sale price in the suggested unit. The lines carry the job's id and
// stay editable like any other line; a suggested part marked non-billable
// becomes a hidden consumable (FR-CAT-08).
func (uc *QuoteUseCase) AddJob(ctx context.Context, req AddJobRequest) (*domain.Quote, error) {
	q, err := uc.Quotes.FindByID(ctx, req.GarageID, req.QuoteID)
	if err != nil {
		return nil, toAppError("add quote job", err)
	}

	if !q.Status.Editable() {
		return nil, toAppError("add quote job", domain.ErrNotEditable)
	}

	job, err := uc.Jobs.Job(ctx, req.GarageID, req.JobID)
	if err != nil {
		return nil, toAppError("add quote job", err)
	}

	jobID := job.ID
	inputs := []LineInput{{
		ItemType: domain.ItemTypeLabor, Description: job.Name, Quantity: "1", UnitPrice: job.LaborPrice, JobID: &jobID,
	}}

	for _, p := range job.Parts {
		partID := p.PartID
		in := LineInput{
			ItemType: domain.ItemTypePart, ItemRefID: &partID, Description: p.Description, Quantity: p.Quantity,
			UnitPrice: p.UnitPrice, JobID: &jobID, NonBillable: p.NonBillable,
		}

		if p.UnitID != "" {
			in.UnitID, in.EnteredQuantity = p.UnitID, p.Quantity
		}

		inputs = append(inputs, in)
	}

	if len(q.Lines)+len(inputs) > domain.MaxLines {
		return nil, toAppError("add quote job", domain.ErrTooManyLines)
	}

	added, err := uc.buildLines(ctx, req.GarageID, inputs)
	if err != nil {
		return nil, toAppError("add quote job", err)
	}

	for i := range added {
		added[i].Position = len(q.Lines) + i + 1
	}

	q.Lines = append(q.Lines, added...)
	q.Recalculate()
	q.UpdatedBy = actorRef(req.ActorID)

	if err := uc.Quotes.ReplaceLines(ctx, q); err != nil {
		return nil, toAppError("add quote job", err)
	}

	return q, nil
}
