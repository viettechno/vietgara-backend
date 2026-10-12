package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

var companyParty = billingparty.Party{
	TaxProfileID: "tp-1", Kind: "company", LegalName: "Cong ty Van Tai A", TaxID: "0301234567", Address: "12 Le Loi",
}

func TestQuoteUseCase_Create_BillingParty(t *testing.T) {
	t.Parallel()

	create := func(t *testing.T, profile *string) (*usecase.QuoteUseCase, *deps, billingparty.Party) {
		t.Helper()

		uc, d := newUseCase(t)
		d.party = func(string, *string) (billingparty.Party, error) { return companyParty, nil }
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(1, nil).Once()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		q, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(), BillingTaxProfileID: profile,
		})
		require.NoError(t, err)

		return uc, d, q.BillingParty
	}

	t.Run("keeps the snapshot of the chosen profile", func(t *testing.T) {
		t.Parallel()

		_, d, party := create(t, new("tp-1"))
		assert.Equal(t, companyParty, party)
		require.Len(t, d.resolved, 1)
		assert.Equal(t, "tp-1", *d.resolved[0])
	})

	t.Run("none chosen asks for the default", func(t *testing.T) {
		t.Parallel()

		_, d, _ := create(t, nil)
		require.Len(t, d.resolved, 1)
		assert.Nil(t, d.resolved[0])
	})

	t.Run("an unusable profile is refused before anything is consumed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(string, *string) (billingparty.Party, error) {
			return billingparty.Party{}, apperror.New(apperror.CodeInvalidArgument, "not the customer's")
		}
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(), BillingTaxProfileID: new("other"),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestQuoteUseCase_Update_BillingParty(t *testing.T) {
	t.Parallel()

	update := func(t *testing.T, req usecase.UpdateQuoteRequest) (*deps, billingparty.Party) {
		t.Helper()

		uc, d := newUseCase(t)
		d.party = func(string, *string) (billingparty.Party, error) { return companyParty, nil }
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote("draft"), nil).Once()

		if req.CustomerID != nil {
			d.Vehicles.EXPECT().Find(mock.Anything, garageID, *req.CustomerID, vehicleID).Return("Binh", "51F-123.45", nil).Once()
		}

		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		req.GarageID, req.QuoteID = garageID, quoteID

		q, err := uc.Update(t.Context(), req)
		require.NoError(t, err)

		return d, q.BillingParty
	}

	t.Run("a chosen profile replaces the party", func(t *testing.T) {
		t.Parallel()

		d, party := update(t, usecase.UpdateQuoteRequest{BillingTaxProfileID: new("tp-1")})
		assert.Equal(t, companyParty, party)
		require.Len(t, d.resolved, 1)
	})

	t.Run("retail is chosen with an empty id", func(t *testing.T) {
		t.Parallel()

		d, _ := update(t, usecase.UpdateQuoteRequest{BillingTaxProfileID: new("")})
		require.Len(t, d.resolved, 1)
		assert.Empty(t, *d.resolved[0])
	})

	t.Run("a new customer takes their own default", func(t *testing.T) {
		t.Parallel()

		d, party := update(t, usecase.UpdateQuoteRequest{CustomerID: new("new-customer")})
		assert.Equal(t, companyParty, party)
		require.Len(t, d.resolved, 1)
		assert.Nil(t, d.resolved[0])
	})

	t.Run("an update of the notes leaves it alone", func(t *testing.T) {
		t.Parallel()

		d, party := update(t, usecase.UpdateQuoteRequest{Notes: new("hello")})
		assert.True(t, party.IsZero())
		assert.Empty(t, d.resolved)
	})

	t.Run("an unusable profile is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(string, *string) (billingparty.Party, error) {
			return billingparty.Party{}, apperror.New(apperror.CodeInvalidArgument, "not the customer's")
		}
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote("draft"), nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID, BillingTaxProfileID: new("x")})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}
