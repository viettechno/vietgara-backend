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

func approvedQuote() *domain.Quote {
	q := existingQuote(domain.StatusApproved)
	q.Lines[0].Decision = domain.DecisionApproved
	q.Lines[1].Decision = domain.DecisionRejected
	q.DecidedAt = &now

	return q
}

func TestQuoteUseCase_Convert(t *testing.T) {
	t.Parallel()

	t.Run("converts the approved lines only", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, garageID, quoteID, vehicleID, "LSC-202609-0001", actorID,
			mock.MatchedBy(func(lines []domain.Line) bool { return len(lines) == 1 && lines[0].ID == "line-1" }),
		).Return("ro-1", nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

		result, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.Equal(t, "ro-1", result.RepairOrderID)
		assert.Equal(t, domain.StatusConverted, result.Quote.Status)
		assert.NotNil(t, result.Quote.ConvertedAt)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

		_, err := uc.Convert(t.Context(), garageID, "missing", actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("not approved", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusSent), nil).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("garage read failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, errBoom).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("plan limit reached", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		planLimit := apperror.New(apperror.CodeFailedPrecondition, "limit reached")
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(planLimit).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("sequencer failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(0, errBoom).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("repair order creation failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, garageID, quoteID, vehicleID, "LSC-202609-0001", actorID, mock.Anything).
			Return("", errBoom).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("repository failure saving the converted quote", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(approvedQuote(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, garageID, quoteID, vehicleID, "LSC-202609-0001", actorID, mock.Anything).
			Return("ro-1", nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
