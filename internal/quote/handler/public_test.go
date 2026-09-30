package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

func TestGetPublicQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	hash := domain.HashPublicToken("a-token")
	e.quotes.EXPECT().FindByPublicToken(anyArg, hash).Return(garageID, quoteID, nil).Once()
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(draftQuote(), nil).Once()

	resp, err := e.srv.GetPublicQuote(t.Context(), &quotev1.GetPublicQuoteRequest{Token: "a-token"})
	require.NoError(t, err)
	assert.Equal(t, "BG-202609-0001", resp.GetQuote().GetNumber())
	assert.Equal(t, "51F-123.45", resp.GetQuote().GetVehicleLicensePlate())
}

func TestGetPublicQuote_InvalidToken(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByPublicToken(anyArg, anyArg).Return("", "", domain.ErrPublicTokenInvalid).Once()

	_, err := e.srv.GetPublicQuote(t.Context(), &quotev1.GetPublicQuoteRequest{Token: "bad-token"})
	require.Error(t, err)
}

func TestDecidePublicQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	hash := domain.HashPublicToken("a-token")
	q := draftQuote()
	q.Status = domain.StatusSent
	q.PublicTokenHash = &hash
	q.Lines = append(q.Lines, domain.Line{ID: "line-2", Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000})

	e.quotes.EXPECT().FindByPublicToken(anyArg, hash).Return(garageID, quoteID, nil).Once()
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(q, nil).Once()
	e.quotes.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, "quote.approved", garageID, "", anyArg).Return(nil).Once()

	resp, err := e.srv.DecidePublicQuote(t.Context(), &quotev1.DecidePublicQuoteRequest{
		Token:     "a-token",
		Decisions: []*quotev1.QuoteLineDecisionInput{{LineId: "line-1", Approved: true}, {LineId: "line-2", Approved: false}},
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, resp.GetQuote().GetStatus())
}

func TestDecidePublicQuote_InvalidToken(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByPublicToken(anyArg, anyArg).Return("", "", domain.ErrPublicTokenInvalid).Once()

	_, err := e.srv.DecidePublicQuote(t.Context(), &quotev1.DecidePublicQuoteRequest{Token: "bad-token"})
	require.Error(t, err)
}
