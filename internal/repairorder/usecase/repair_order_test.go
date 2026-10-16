package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

func TestRepairOrderUseCase_Create(t *testing.T) {
	t.Parallel()

	t.Run("creates an ad-hoc repair order", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		ro, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{
			GarageID: garageID, ActorID: actorID, VehicleID: vehicleID, Lines: oneLine(),
		})
		require.NoError(t, err)
		require.Equal(t, domain.StatusOpen, ro.Status)
		require.Equal(t, "An Nguyen", ro.CustomerName)
		require.Equal(t, "51F-123.45", ro.VehicleLicensePlate)
		require.Equal(t, "LSC-202609-0001", ro.Number)
		require.Equal(t, int64(250_000), ro.TotalAmount)
	})

	t.Run("no lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{GarageID: garageID, VehicleID: vehicleID})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("vehicle not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, vehicleID).Return("", "", domain.ErrVehicleRequired).Once()

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{
			GarageID: garageID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("plan limit reached", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()

		planLimit := apperror.New(apperror.CodeFailedPrecondition, "limit reached")
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(planLimit).Once()

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{
			GarageID: garageID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("garage read failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{
			GarageID: garageID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{
			GarageID: garageID, VehicleID: vehicleID, Lines: oneLine(),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestRepairOrderUseCase_CreateFromQuote(t *testing.T) {
	t.Parallel()

	lines := []domain.Line{
		{Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000},
		{Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000},
	}

	t.Run("creates a draft repair order from approved lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		ro, err := uc.CreateFromQuote(t.Context(), usecase.CreateFromQuoteRequest{
			GarageID: garageID, QuoteID: quoteID, ActorID: actorID, VehicleID: vehicleID, CustomerName: "An Nguyen",
			VehiclePlate: "51F-123.45", Number: "LSC-202609-0001", Lines: lines,
		})
		require.NoError(t, err)
		require.Equal(t, domain.StatusOpen, ro.Status)
		require.Equal(t, int64(730_000), ro.TotalAmount)
		require.Equal(t, quoteID, *ro.QuoteID)
		require.Equal(t, "Cong ty TNHH An Phat", ro.Issuer.Name, "the garage's identity is snapshotted")
		require.Len(t, ro.Lines, 2)
		require.NotEmpty(t, ro.ID)
		require.NotEmpty(t, ro.Lines[0].ID)
		require.Equal(t, domain.LineNotStarted, ro.Lines[0].Status)
	})

	t.Run("quote already converted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrAlreadyConverted).Once()

		_, err := uc.CreateFromQuote(t.Context(), usecase.CreateFromQuoteRequest{
			GarageID: garageID, QuoteID: quoteID, VehicleID: vehicleID,
		})
		require.ErrorIs(t, err, domain.ErrAlreadyConverted)
	})
}

func TestRepairOrderUseCase_Get(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

	ro, err := uc.Get(t.Context(), garageID, repairOrderID)
	require.NoError(t, err)
	require.Equal(t, repairOrderID, ro.ID)

	d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrRepairOrderNotFound).Once()

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestRepairOrderUseCase_List(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := []*domain.RepairOrder{openRepairOrder()}
	d.RepairOrders.EXPECT().
		List(mock.Anything, garageID, usecase.ListFilter{Status: domain.StatusOpen, Limit: 20, Offset: 0}).
		Return(want, 1, nil).Once()

	page, err := uc.List(t.Context(), usecase.ListRepairOrdersRequest{GarageID: garageID, Status: domain.StatusOpen})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, want, page.Items)

	d.RepairOrders.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

	_, err = uc.List(t.Context(), usecase.ListRepairOrdersRequest{GarageID: garageID})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestRepairOrderUseCase_ListForVehicle(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := []*domain.RepairOrder{openRepairOrder()}
	d.RepairOrders.EXPECT().ListForVehicle(mock.Anything, garageID, vehicleID).Return(want, nil).Once()

	got, err := uc.ListForVehicle(t.Context(), garageID, vehicleID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestRepairOrderUseCase_Create_PartLineInPackagingUnit(t *testing.T) {
	t.Parallel()

	const (
		partID = "77777777-7777-7777-7777-777777777777"
		unitID = "88888888-8888-8888-8888-888888888888"
	)

	lines := func() []usecase.LineInput {
		return []usecase.LineInput{{
			ItemType: domain.ItemTypePart, ItemRefID: &[]string{partID}[0], Description: "Beer", Quantity: "999",
			UnitID: unitID, EnteredQuantity: "2", UnitPrice: 15_000,
		}}
	}

	t.Run("the base quantity is derived from the typed amount", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		unit, entered := unitID, "2"

		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(partquantity.Resolved{Base: "48", UnitID: &unit, Entered: &entered}, nil).Once()
		d.Vehicles.EXPECT().Find(mock.Anything, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
		d.RepairOrders.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		ro, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{GarageID: garageID, VehicleID: vehicleID, Lines: lines()})
		require.NoError(t, err)
		require.Equal(t, "48", ro.Lines[0].Quantity)
		require.Equal(t, unitID, *ro.Lines[0].UnitID)
		require.Equal(t, "2", *ro.Lines[0].EnteredQuantity)
		require.Equal(t, int64(720_000), ro.Lines[0].LineTotal)
	})

	t.Run("a unit on a labor line is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		in := lines()
		in[0].ItemType = domain.ItemTypeLabor

		_, err := uc.Create(t.Context(), usecase.CreateRepairOrderRequest{GarageID: garageID, VehicleID: vehicleID, Lines: in})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}
