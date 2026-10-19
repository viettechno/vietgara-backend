package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
)

const jobID = "77777777-7777-7777-7777-777777777777"

func TestAddQuoteJob(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(draftQuote(), nil).Once()
	e.jobs.EXPECT().Job(anyArg, garageID, jobID).Return(usecase.Job{ID: jobID, Name: "Oil change", LaborPrice: 200_000}, nil).Once()
	e.quotes.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.AddQuoteJob(signedIn(t), &quotev1.AddQuoteJobRequest{GarageId: garageID, QuoteId: quoteID, JobId: jobID})
	require.NoError(t, err)

	lines := resp.GetQuote().GetLines()
	require.Greater(t, len(lines), 1)
	assert.Equal(t, jobID, lines[len(lines)-1].GetJobId())
}

func TestAddQuoteJob_NotFound(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(nil, domain.ErrQuoteNotFound).Once()

	_, err := e.srv.AddQuoteJob(signedIn(t), &quotev1.AddQuoteJobRequest{GarageId: garageID, QuoteId: quoteID, JobId: jobID})
	require.Error(t, err)
}

func TestPublicQuoteHidesConsumables(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	q := draftQuote()
	q.Lines = append(q.Lines, domain.Line{ID: "grease", ItemType: domain.ItemTypePart, Description: "Grease", Quantity: "1", NonBillable: true})

	e.quotes.EXPECT().FindByPublicToken(anyArg, domain.HashPublicToken("a-token")).Return(garageID, quoteID, nil).Once()
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(q, nil).Once()

	resp, err := e.srv.GetPublicQuote(t.Context(), &quotev1.GetPublicQuoteRequest{Token: "a-token"})
	require.NoError(t, err)

	for _, line := range resp.GetQuote().GetLines() {
		assert.NotEqual(t, "grease", line.GetId())
	}

	// The staff view keeps it, marked.
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(q, nil).Once()

	staff, err := e.srv.GetQuote(signedIn(t), &quotev1.GetQuoteRequest{GarageId: garageID, QuoteId: quoteID})
	require.NoError(t, err)

	last := staff.GetQuote().GetLines()[len(staff.GetQuote().GetLines())-1]
	assert.True(t, last.GetNonBillable())
}
