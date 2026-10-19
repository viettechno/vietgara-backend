package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

const (
	jobID  = "77777777-7777-7777-7777-777777777777"
	partID = "88888888-8888-8888-8888-888888888888"
	caseID = "99999999-9999-9999-9999-999999999999"
)

func oilChange() usecase.Job {
	return usecase.Job{
		ID: jobID, Name: "Oil change", LaborPrice: 200_000,
		Parts: []usecase.JobPart{
			{PartID: partID, Description: "Engine oil 5W-30", Quantity: "4", UnitPrice: 90_000},
			{PartID: "grease-part", Description: "Grease", Quantity: "1", UnitPrice: 20_000, NonBillable: true},
		},
	}
}

func addJob(t *testing.T, uc *usecase.QuoteUseCase) (*domain.Quote, error) {
	t.Helper()

	return uc.AddJob(t.Context(), usecase.AddJobRequest{GarageID: garageID, QuoteID: quoteID, JobID: jobID, ActorID: actorID})
}

func TestQuoteUseCase_AddJob(t *testing.T) {
	t.Parallel()

	t.Run("adds the labor line and the part lines, hiding the consumable", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).Return(oilChange(), nil).Once()
		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		q, err := addJob(t, uc)
		require.NoError(t, err)
		require.Len(t, q.Lines, 5)

		labor, oil, grease := q.Lines[2], q.Lines[3], q.Lines[4]
		assert.Equal(t, domain.ItemTypeLabor, labor.ItemType)
		assert.Equal(t, int64(200_000), labor.LineTotal)
		assert.Equal(t, jobID, *labor.JobID)
		assert.Equal(t, 3, labor.Position)

		assert.Equal(t, int64(360_000), oil.LineTotal)
		assert.Equal(t, partID, *oil.ItemRefID)
		assert.False(t, oil.NonBillable)

		assert.True(t, grease.NonBillable)
		assert.Equal(t, int64(0), grease.LineTotal)
		assert.Equal(t, int64(0), grease.UnitPrice)

		// 730,000 already on the quote, plus labor and oil; the consumable is free.
		assert.Equal(t, int64(730_000+200_000+360_000), q.TotalAmount)
	})

	t.Run("a part suggested in a packaging unit is typed in that unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).Return(usecase.Job{
			ID: jobID, Name: "Oil change", Parts: []usecase.JobPart{
				{PartID: partID, Description: "Oil", UnitID: caseID, Quantity: "2", UnitPrice: 2_000_000},
			},
		}, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, caseID, "2").
			Return(partquantity.Resolved{Base: "48", UnitID: new(caseID), Entered: new("2"), UnitAbbreviation: "case"}, nil).Once()
		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		q, err := addJob(t, uc)
		require.NoError(t, err)
		assert.Equal(t, "48", q.Lines[3].Quantity)
		assert.Equal(t, "2", *q.Lines[3].EnteredQuantity)
	})

	t.Run("a quote that is no longer a draft is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusSent), nil).Once()

		_, err := addJob(t, uc)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("a job that is unknown or inactive is refused as the catalogue says", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).
			Return(usecase.Job{}, apperror.New(apperror.CodeFailedPrecondition, "inactive")).Once()

		_, err := addJob(t, uc)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("the 100-line limit holds", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)

		for len(q.Lines) < domain.MaxLines-1 {
			q.Lines = append(q.Lines, q.Lines[0])
		}

		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).Return(oilChange(), nil).Once()

		_, err := addJob(t, uc)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(nil, domain.ErrQuoteNotFound).Once()

		_, err := addJob(t, uc)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).Return(oilChange(), nil).Once()
		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = addJob(t, uc)
		assertAppError(t, err, apperror.CodeInternal)

		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Jobs.EXPECT().Job(mock.Anything, garageID, jobID).Return(usecase.Job{ID: jobID, Name: " "}, nil).Once()

		_, err = addJob(t, uc)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestQuoteUseCase_HiddenConsumables(t *testing.T) {
	t.Parallel()

	create := func(t *testing.T, uc *usecase.QuoteUseCase, lines []usecase.LineInput) error {
		t.Helper()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, ActorID: actorID, CustomerID: customerID, VehicleID: vehicleID, Lines: lines,
		})

		return err
	}

	prime := func(d *deps) {
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Maybe()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An", "51F", nil).Maybe()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Maybe()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, mock.Anything, mock.Anything).Return(1, nil).Maybe()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
	}

	part := func() usecase.LineInput {
		ref := partID

		return usecase.LineInput{
			ItemType: domain.ItemTypePart, ItemRefID: &ref, Description: "Grease", Quantity: "1", UnitPrice: 20_000, NonBillable: true,
		}
	}

	t.Run("a hidden consumable is stored at zero", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		prime(d)

		require.NoError(t, create(t, uc, append(oneLine(), part())))
	})

	t.Run("only a catalogue part line can be hidden", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		prime(d)

		labor := part()
		labor.ItemType = domain.ItemTypeLabor

		noRef := part()
		noRef.ItemRefID = nil

		for _, bad := range []usecase.LineInput{labor, noRef} {
			assertAppError(t, create(t, uc, append(oneLine(), bad)), apperror.CodeInvalidArgument)
		}
	})

	t.Run("a quote needs a billable line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		prime(d)

		assertAppError(t, create(t, uc, []usecase.LineInput{part()}), apperror.CodeInvalidArgument)
	})

	t.Run("a job reference must be an id", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		prime(d)

		line := oneLine()
		line[0].JobID = new("not-an-id")
		assertAppError(t, create(t, uc, line), apperror.CodeInvalidArgument)

		line[0].JobID = new(jobID)
		require.NoError(t, create(t, uc, line))

		line[0].JobID = new("")
		require.NoError(t, create(t, uc, line))
	})
}

func quoteWithJob(hiddenJob *string) *domain.Quote {
	q := existingQuote(domain.StatusSent)
	job := jobID
	q.Lines = []domain.Line{
		{ID: "labor", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 200_000, LineTotal: 200_000, JobID: &job, Decision: domain.DecisionPending},
		{ID: "oil", Position: 2, ItemType: domain.ItemTypePart, Description: "Oil", Quantity: "4", UnitPrice: 90_000, LineTotal: 360_000, JobID: &job, Decision: domain.DecisionPending},
		{ID: "other", Position: 3, ItemType: domain.ItemTypeLabor, Description: "Wash", Quantity: "1", UnitPrice: 50_000, LineTotal: 50_000, Decision: domain.DecisionPending},
		{ID: "grease", Position: 4, ItemType: domain.ItemTypePart, Description: "Grease", Quantity: "1", NonBillable: true, JobID: hiddenJob, Decision: domain.DecisionPending},
	}

	return q
}

func TestQuoteUseCase_DecisionsFollowHiddenConsumables(t *testing.T) {
	t.Parallel()

	decide := func(t *testing.T, hiddenJob *string, approved map[string]bool) map[string]domain.LineDecision {
		t.Helper()

		uc, d := newUseCase(t)
		q := quoteWithJob(hiddenJob)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, mock.Anything, garageID, "", mock.Anything).Return(nil).Once()

		var inputs []usecase.LineDecisionInput
		for _, id := range []string{"labor", "oil", "other"} {
			inputs = append(inputs, usecase.LineDecisionInput{LineID: id, Approved: approved[id]})
		}

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, inputs)
		require.NoError(t, err)

		out := map[string]domain.LineDecision{}
		for _, l := range q.Lines {
			out[l.ID] = l.Decision
		}

		return out
	}

	t.Run("a consumable is used when its job is approved", func(t *testing.T) {
		t.Parallel()

		got := decide(t, new(jobID), map[string]bool{"labor": true, "oil": false, "other": false})
		assert.Equal(t, domain.DecisionApproved, got["grease"])
	})

	t.Run("a consumable is dropped with its job", func(t *testing.T) {
		t.Parallel()

		got := decide(t, new(jobID), map[string]bool{"other": true})
		assert.Equal(t, domain.DecisionRejected, got["grease"])
		assert.Equal(t, domain.DecisionRejected, got["labor"])
	})

	t.Run("a free-standing consumable follows the quote", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, domain.DecisionApproved, decide(t, nil, map[string]bool{"other": true})["grease"])
		assert.Equal(t, domain.DecisionRejected, decide(t, nil, map[string]bool{})["grease"])
	})

	t.Run("the customer decides on billable lines only", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(quoteWithJob(new(jobID)), nil).Once()

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{
			{LineID: "labor", Approved: true},
			{LineID: "oil", Approved: true},
			{LineID: "other", Approved: true},
			{LineID: "grease", Approved: true},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}
