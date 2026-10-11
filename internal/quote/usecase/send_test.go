package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

func TestQuoteUseCase_Send(t *testing.T) {
	t.Parallel()

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

		_, err := uc.Send(t.Context(), garageID, "missing", actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("not sendable once converted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusConverted), nil).Once()

		_, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("manual mode sends no link or e-mail", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		q.ApprovalMode = domain.ApprovalModeManual
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.MatchedBy(func(saved *domain.Quote) bool {
			return saved.PublicTokenHash == nil && saved.PublicTokenExpiresAt == nil
		})).Return(nil).Once()

		result, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.StatusSent, result.Quote.Status)
		assert.Empty(t, result.PublicURL)
		assert.False(t, result.Emailed)
	})

	t.Run("link mode e-mails the customer when they have an address", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.MatchedBy(func(saved *domain.Quote) bool {
			return saved.PublicTokenHash != nil && saved.PublicTokenExpiresAt != nil
		})).Return(nil).Once()

		email := "customer@vietgara.test"
		d.Customers.EXPECT().Email(mock.Anything, garageID, customerID).Return(&email, nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(nil).Once()

		result, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.StatusSent, result.Quote.Status)
		assert.Contains(t, result.PublicURL, "https://vietgara.viettechno.com/public/quotes/")
		assert.True(t, result.Emailed)
	})

	t.Run("link mode with no e-mail on file", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Customers.EXPECT().Email(mock.Anything, garageID, customerID).Return(nil, nil).Once()

		result, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.NotEmpty(t, result.PublicURL)
		assert.False(t, result.Emailed)
	})

	t.Run("a failed e-mail lookup does not fail sending", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Customers.EXPECT().Email(mock.Anything, garageID, customerID).Return(nil, errBoom).Once()

		result, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.False(t, result.Emailed)
	})
}

func TestQuoteUseCase_SendFailuresAndIssuer(t *testing.T) {
	t.Parallel()

	t.Run("a failed delivery does not fail sending", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

		email := "customer@vietgara.test"
		d.Customers.EXPECT().Email(mock.Anything, garageID, customerID).Return(&email, nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()

		result, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
		assert.False(t, result.Emailed)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusSent)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("the quote carries the garage's identity at the moment it is sent (FR-TEN-06)", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		q := existingQuote(domain.StatusDraft)
		q.Issuer = issuer.Issuer{Name: "Old name"}
		garage := defaultGarage()
		garage.Issuer = issuer.Issuer{Name: "Cong ty TNHH An Phat", TaxCode: "0301234567"}

		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garage, nil).Once()
		d.Quotes.EXPECT().Save(mock.Anything, mock.MatchedBy(func(saved *domain.Quote) bool {
			return saved.Issuer.Name == "Cong ty TNHH An Phat" && saved.Issuer.TaxCode == "0301234567"
		})).Return(nil).Once()
		d.Customers.EXPECT().Email(mock.Anything, garageID, customerID).Return(nil, nil).Once()

		_, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		require.NoError(t, err)
	})

	t.Run("a garage read failure fails sending", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, errBoom).Once()

		_, err := uc.Send(t.Context(), garageID, quoteID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
