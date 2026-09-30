package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func sentQuote() *domain.Quote {
	q := existingQuote(domain.StatusSent)
	q.SentAt = &now

	return q
}

func TestQuoteUseCase_RecordQuoteDecision(t *testing.T) {
	t.Parallel()

	t.Run("approves at least one line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(sentQuote(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, "quote.approved", garageID, "", mock.Anything).Return(nil).Once()

		q, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{
			{LineID: "line-1", Approved: true}, {LineID: "line-2", Approved: false},
		})
		require.NoError(t, err)
		assert.Equal(t, domain.StatusApproved, q.Status)
		assert.Equal(t, domain.DecisionApproved, q.Lines[0].Decision)
		assert.Equal(t, domain.DecisionRejected, q.Lines[1].Decision)
		assert.Nil(t, q.PublicTokenHash)
		assert.Equal(t, actorID, *q.UpdatedBy)
	})

	t.Run("rejects when every line is rejected", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(sentQuote(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, "quote.rejected", garageID, "", mock.Anything).Return(nil).Once()

		q, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{
			{LineID: "line-1", Approved: false}, {LineID: "line-2", Approved: false},
		})
		require.NoError(t, err)
		assert.Equal(t, domain.StatusRejected, q.Status)
	})

	t.Run("not decidable while still a draft", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, nil)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("missing a decision for a line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(sentQuote(), nil).Once()

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{{LineID: "line-1", Approved: true}})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("decision for an unknown line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(sentQuote(), nil).Once()

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{
			{LineID: "line-1", Approved: true}, {LineID: "unknown", Approved: true},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(sentQuote(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.RecordQuoteDecision(t.Context(), garageID, quoteID, actorID, []usecase.LineDecisionInput{
			{LineID: "line-1", Approved: true}, {LineID: "line-2", Approved: true},
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestQuoteUseCase_GetPublicQuote(t *testing.T) {
	t.Parallel()

	const token = "a-plaintext-token" //nolint:gosec // G101: a fake approval-link token, not a credential

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		hash := domain.HashPublicToken(token)
		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, hash).Return(garageID, quoteID, nil).Once()

		want := sentQuote()
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(want, nil).Once()

		q, err := uc.GetPublicQuote(t.Context(), token)
		require.NoError(t, err)
		assert.Same(t, want, q)
	})

	t.Run("unknown token", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, mock.Anything).Return("", "", domain.ErrPublicTokenInvalid).Once()

		_, err := uc.GetPublicQuote(t.Context(), token)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code)
		assert.Equal(t, usecase.ReasonPublicTokenInvalid, appErr.Reason)
	})
}

func TestQuoteUseCase_DecidePublicQuote(t *testing.T) {
	t.Parallel()

	const token = "a-plaintext-token" //nolint:gosec // G101: a fake approval-link token, not a credential

	decisions := []usecase.LineDecisionInput{{LineID: "line-1", Approved: true}, {LineID: "line-2", Approved: true}}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		hash := domain.HashPublicToken(token)
		q := sentQuote()
		q.PublicTokenHash = &hash

		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, hash).Return(garageID, quoteID, nil).Once()
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, "quote.approved", garageID, "", mock.Anything).Return(nil).Once()

		result, err := uc.DecidePublicQuote(t.Context(), token, decisions)
		require.NoError(t, err)
		assert.Equal(t, domain.StatusApproved, result.Status)
		assert.Nil(t, result.PublicTokenHash)
	})

	t.Run("manual-mode quote rejects the public flow", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		hash := domain.HashPublicToken(token)
		q := sentQuote()
		q.ApprovalMode = domain.ApprovalModeManual

		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, hash).Return(garageID, quoteID, nil).Once()
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()

		_, err := uc.DecidePublicQuote(t.Context(), token, decisions)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, usecase.ReasonPublicTokenInvalid, appErr.Reason)
	})

	t.Run("a stale token no longer matches the quote's current one", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		hash := domain.HashPublicToken(token)
		q := sentQuote()
		freshHash := domain.HashPublicToken("a-newer-token")
		q.PublicTokenHash = &freshHash

		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, hash).Return(garageID, quoteID, nil).Once()
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()

		_, err := uc.DecidePublicQuote(t.Context(), token, decisions)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, usecase.ReasonPublicTokenInvalid, appErr.Reason)
	})

	t.Run("unknown token", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByPublicToken(mock.Anything, mock.Anything).Return("", "", domain.ErrPublicTokenInvalid).Once()

		_, err := uc.DecidePublicQuote(t.Context(), token, decisions)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, usecase.ReasonPublicTokenInvalid, appErr.Reason)
	})
}
