package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

func TestSendQuote_NotFound(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

	_, err := e.srv.SendQuote(signedIn(t), &quotev1.SendQuoteRequest{GarageId: garageID, QuoteId: "missing"})
	require.Error(t, err)
}

func TestRecordQuoteDecision_NotFound(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

	_, err := e.srv.RecordQuoteDecision(signedIn(t), &quotev1.RecordQuoteDecisionRequest{GarageId: garageID, QuoteId: "missing"})
	require.Error(t, err)
}

func TestConvertQuote_NotFound(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

	_, err := e.srv.ConvertQuote(signedIn(t), &quotev1.ConvertQuoteRequest{GarageId: garageID, QuoteId: "missing"})
	require.Error(t, err)
}

func TestSendQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(draftQuote(), nil).Once()
	e.quotes.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.customers.EXPECT().Email(anyArg, garageID, customerID).Return(nil, nil).Once()

	resp, err := e.srv.SendQuote(signedIn(t), &quotev1.SendQuoteRequest{GarageId: garageID, QuoteId: quoteID})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, resp.GetQuote().GetStatus())
	assert.NotEmpty(t, resp.GetPublicUrl())
	assert.False(t, resp.GetEmailed())
}

func TestRecordQuoteDecision(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	q := draftQuote()
	q.Status = domain.StatusSent
	q.Lines = append(q.Lines, domain.Line{ID: "line-2", Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000})

	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(q, nil).Once()
	e.quotes.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, "quote.approved", garageID, "", anyArg).Return(nil).Once()

	resp, err := e.srv.RecordQuoteDecision(signedIn(t), &quotev1.RecordQuoteDecisionRequest{
		GarageId: garageID, QuoteId: quoteID,
		Decisions: []*quotev1.QuoteLineDecisionInput{{LineId: "line-1", Approved: true}, {LineId: "line-2", Approved: false}},
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, resp.GetQuote().GetStatus())
}

func TestConvertQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	q := draftQuote()
	q.Status = domain.StatusApproved
	q.Lines[0].Decision = domain.DecisionApproved

	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(q, nil).Once()
	e.garages.EXPECT().Get(anyArg, garageID).Return(garageInfoWithRepairOrderTemplate(), nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "repair_order", "LSC-{SEQ:4}").Return(1, nil).Once()
	e.repairs.EXPECT().Create(anyArg, garageID, quoteID, vehicleID, "LSC-0001", actorID, anyArg).Return("ro-1", nil).Once()
	e.quotes.EXPECT().Save(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.ConvertQuote(signedIn(t), &quotev1.ConvertQuoteRequest{GarageId: garageID, QuoteId: quoteID})
	require.NoError(t, err)
	assert.Equal(t, "ro-1", resp.GetRepairOrderId())
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_CONVERTED, resp.GetQuote().GetStatus())
}
