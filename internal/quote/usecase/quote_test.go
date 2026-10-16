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

func TestQuoteUseCase_Create(t *testing.T) {
	t.Parallel()

	t.Run("creates a draft, numbered and defaulted to the garage's approval mode", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(1, nil).Once()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		q, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, ActorID: actorID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
		})
		require.NoError(t, err)
		assert.Equal(t, "BG-202609-0001", q.Number)
		assert.Equal(t, domain.StatusDraft, q.Status)
		assert.Equal(t, domain.ApprovalModeLink, q.ApprovalMode)
		assert.Equal(t, "An Nguyen", q.CustomerName)
		assert.Equal(t, int64(250_000), q.TotalAmount)
		assert.Equal(t, actorID, *q.CreatedBy)
	})

	t.Run("an explicit approval mode overrides the garage default", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(1, nil).Once()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		manual := domain.ApprovalModeManual
		q, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, ApprovalMode: &manual, Lines: oneLine(),
		})
		require.NoError(t, err)
		assert.Equal(t, domain.ApprovalModeManual, q.ApprovalMode)
	})

	t.Run("garage read failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("no lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("vehicle does not belong to the customer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("", "", domain.ErrCustomerRequired).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestQuoteUseCase_Create_MoreValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid approval mode", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()

		bad := domain.ApprovalMode("email")
		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, ApprovalMode: &bad, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("notes too long", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
			Notes: string(make([]rune, domain.MaxNotesLen+1)),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("plan limit reached", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()

		planLimit := apperror.New(apperror.CodeFailedPrecondition, "limit reached")
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(planLimit).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("sequencer failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(0, errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(1, nil).Once()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestQuoteUseCase_Get(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := existingQuote(domain.StatusDraft)
	d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(want, nil).Once()
	d.Quotes.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

	q, err := uc.Get(t.Context(), garageID, quoteID)
	require.NoError(t, err)
	assert.Same(t, want, q)

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestQuoteUseCase_Update(t *testing.T) {
	t.Parallel()

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("not editable once sent", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusSent), nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("changes the vehicle and replaces the lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, "new-vehicle").Return("An Nguyen", "51G-999.99", nil).Once()
		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		newVehicle := "new-vehicle"
		notes := "customer prefers pickup after 5pm"
		lines := []usecase.LineInput{{ItemType: domain.ItemTypePart, Description: "Brake pads", Quantity: "2", UnitPrice: 300_000}}

		q, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{
			GarageID: garageID, QuoteID: quoteID, ActorID: actorID, VehicleID: &newVehicle, Notes: &notes, Lines: &lines,
		})
		require.NoError(t, err)
		assert.Equal(t, "51G-999.99", q.VehicleLicensePlate)
		assert.Equal(t, notes, q.Notes)
		assert.Equal(t, int64(600_000), q.TotalAmount)
		assert.Len(t, q.Lines, 1)
		assert.Equal(t, actorID, *q.UpdatedBy)
	})

	t.Run("invalid approval mode", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()

		bad := domain.ApprovalMode("x")
		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID, ApprovalMode: &bad})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("notes too long", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()

		notes := string(make([]rune, domain.MaxNotesLen+1))
		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID, Notes: &notes})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("invalid lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()

		empty := []usecase.LineInput{}
		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID, Lines: &empty})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(existingQuote(domain.StatusDraft), nil).Once()
		d.Quotes.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(errBoom).Once()

		notes := "x"
		_, err := uc.Update(t.Context(), usecase.UpdateQuoteRequest{GarageID: garageID, QuoteID: quoteID, Notes: &notes})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestQuoteUseCase_List(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := []*domain.Quote{existingQuote(domain.StatusDraft)}
	d.Quotes.EXPECT().List(mock.Anything, garageID, usecase.ListFilter{
		CustomerID: customerID, Status: domain.StatusDraft, Limit: 5, Offset: 5,
	}).Return(want, 6, nil).Once()
	d.Quotes.EXPECT().List(mock.Anything, "garage-2", mock.Anything).Return(nil, 0, errBoom).Once()

	page, err := uc.List(t.Context(), usecase.ListQuotesRequest{
		PageRequest: usecase.PageRequest{Page: 2, PageSize: 5}, GarageID: garageID, CustomerID: customerID, Status: domain.StatusDraft,
	})
	require.NoError(t, err)
	assert.Equal(t, 6, page.Total)
	assert.Equal(t, want, page.Items)

	_, err = uc.List(t.Context(), usecase.ListQuotesRequest{GarageID: "garage-2"})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestQuoteUseCase_Delete(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Quotes.EXPECT().SoftDelete(mock.Anything, garageID, quoteID, actorID).Return(nil).Once()
	d.Quotes.EXPECT().SoftDelete(mock.Anything, garageID, "missing", actorID).Return(domain.ErrQuoteNotFound).Once()

	require.NoError(t, uc.Delete(t.Context(), garageID, quoteID, actorID))
	assertAppError(t, uc.Delete(t.Context(), garageID, "missing", actorID), apperror.CodeNotFound)
}

func TestQuoteUseCase_Create_PartLineInPackagingUnit(t *testing.T) {
	t.Parallel()

	const (
		partID = "77777777-7777-7777-7777-777777777777"
		unitID = "88888888-8888-8888-8888-888888888888"
	)

	line := func(mutate func(*usecase.LineInput)) []usecase.LineInput {
		in := usecase.LineInput{
			ItemType: domain.ItemTypePart, ItemRefID: &[]string{partID}[0], Description: "Beer", Quantity: "999",
			UnitID: unitID, EnteredQuantity: "2", UnitPrice: 15_000,
		}
		mutate(&in)

		return []usecase.LineInput{in}
	}

	expectCreate := func(d *deps) {
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "quote", "BG-202609-{SEQ:4}").Return(1, nil).Once()
		d.Quotes.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
	}

	t.Run("the base quantity is derived from the typed amount", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		unit, entered := unitID, "2"
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(partquantity.Resolved{Base: "48", UnitID: &unit, Entered: &entered}, nil).Once()
		expectCreate(d)

		q, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: line(func(*usecase.LineInput) {}),
		})
		require.NoError(t, err)
		require.Len(t, q.Lines, 1)
		assert.Equal(t, "48", q.Lines[0].Quantity, "the client's quantity is replaced")
		assert.Equal(t, unitID, *q.Lines[0].UnitID)
		assert.Equal(t, "2", *q.Lines[0].EnteredQuantity)
		assert.Equal(t, int64(720_000), q.Lines[0].LineTotal, "48 x 15,000")
	})

	t.Run("a unit needs a part, and an amount", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(*usecase.LineInput){
			"labor line": func(in *usecase.LineInput) { in.ItemType = domain.ItemTypeLabor },
			"no part":    func(in *usecase.LineInput) { in.ItemRefID = nil },
			"no amount":  func(in *usecase.LineInput) { in.EnteredQuantity = "" },
		} {
			uc, d := newUseCase(t)
			d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

			_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
				GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: line(mutate),
			})
			assertAppError(t, err, apperror.CodeInvalidArgument)

			_ = name
		}
	})

	t.Run("the catalogue's refusal is passed on", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(partquantity.Resolved{}, apperror.New(apperror.CodeNotFound, "part not found")).Once()

		_, err := uc.Create(t.Context(), usecase.CreateQuoteRequest{
			GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, Lines: line(func(*usecase.LineInput) {}),
		})
		assertAppError(t, err, apperror.CodeNotFound)
	})
}
