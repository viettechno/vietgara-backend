package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	vehicleID     = "22222222-2222-2222-2222-222222222222"
	newCustomerID = "33333333-3333-3333-3333-333333333333"
)

func existingVehicle() *domain.Vehicle {
	year := 2019

	return &domain.Vehicle{
		ID: vehicleID, GarageID: garageID, CustomerID: customerID, CustomerName: "Tran Binh",
		LicensePlate: "51F-123.45", Make: new("Toyota"), Year: &year,
	}
}

func newVehicleUseCase(t *testing.T) (*usecase.VehicleUseCase, *mocks.MockVehicleRepository, *mocks.MockCustomerRepository) {
	t.Helper()

	vehicles := mocks.NewMockVehicleRepository(t)
	customers := mocks.NewMockCustomerRepository(t)

	return usecase.NewVehicleUseCase(vehicles, customers, nil, clock()), vehicles, customers
}

func TestVehicleUseCase_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       usecase.CreateVehicleRequest
		ownerErr  error
		createErr error
		wantCode  apperror.Code
	}{
		{
			name: "normalizes and stores the vehicle",
			req: usecase.CreateVehicleRequest{
				CustomerID: customerID, LicensePlate: "51f-123.45", Make: new(" Toyota "), Model: new("Vios"),
				Year: new(2020), VIN: new("mhf123"), EngineNumber: new(""),
			},
		},
		{name: "requires a plate", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: " "}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad plate", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: "1-2"}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a future year", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: "51F12345", Year: new(2030)}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long VIN", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: "51F12345", VIN: new(string(make([]rune, 51)))}, wantCode: apperror.CodeInvalidArgument},
		{name: "requires an owner", req: usecase.CreateVehicleRequest{LicensePlate: "51F12345"}, wantCode: apperror.CodeInvalidArgument},
		{name: "unknown owner", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: "51F12345"}, ownerErr: domain.ErrCustomerNotFound, wantCode: apperror.CodeNotFound},
		{name: "taken plate", req: usecase.CreateVehicleRequest{CustomerID: customerID, LicensePlate: "51F12345"}, createErr: domain.ErrLicensePlateTaken, wantCode: apperror.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc, vehicles, customers := newVehicleUseCase(t)
			tt.req.GarageID, tt.req.ActorID = garageID, actorID
			reachesOwner := tt.wantCode == "" || tt.ownerErr != nil || tt.createErr != nil

			if reachesOwner {
				owner := existingCustomer()
				if tt.ownerErr != nil {
					owner = nil
				}

				customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(owner, tt.ownerErr).Once()
			}

			if tt.wantCode == "" || tt.createErr != nil {
				vehicles.EXPECT().Create(mock.Anything, mock.Anything).Return(tt.createErr).Once()
			}

			v, err := uc.Create(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "51F-123.45", v.LicensePlate)
			assert.Equal(t, "Toyota", *v.Make)
			assert.Equal(t, 2020, *v.Year)
			assert.Equal(t, "MHF123", *v.VIN)
			assert.Nil(t, v.EngineNumber)
			assert.Equal(t, "Tran Binh", v.CustomerName)
			assert.Equal(t, actorID, *v.CreatedBy)
		})
	}
}

func TestVehicleUseCase_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       usecase.UpdateVehicleRequest
		findErr   error
		ownerErr  error
		updateErr error
		wantCode  apperror.Code
		check     func(t *testing.T, v *domain.Vehicle)
	}{
		{
			name: "clears the year and keeps the owner",
			req:  usecase.UpdateVehicleRequest{Year: new(0), Make: new(""), CustomerID: new(customerID)},
			check: func(t *testing.T, v *domain.Vehicle) {
				t.Helper()
				assert.Nil(t, v.Year)
				assert.Nil(t, v.Make)
				assert.Equal(t, customerID, v.CustomerID)
			},
		},
		{
			name: "transfers the vehicle",
			req:  usecase.UpdateVehicleRequest{CustomerID: new(newCustomerID), LicensePlate: new("30a 99999")},
			check: func(t *testing.T, v *domain.Vehicle) {
				t.Helper()
				assert.Equal(t, newCustomerID, v.CustomerID)
				assert.Equal(t, "30A 99999", v.LicensePlate)
			},
		},
		{name: "unknown vehicle", findErr: domain.ErrVehicleNotFound, wantCode: apperror.CodeNotFound},
		{name: "invalid plate", req: usecase.UpdateVehicleRequest{LicensePlate: new("")}, wantCode: apperror.CodeInvalidArgument},
		{name: "unknown new owner", req: usecase.UpdateVehicleRequest{CustomerID: new(newCustomerID)}, ownerErr: domain.ErrCustomerNotFound, wantCode: apperror.CodeNotFound},
		{name: "storage failure", updateErr: errBoom, wantCode: apperror.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc, vehicles, customers := newVehicleUseCase(t)
			tt.req.GarageID, tt.req.VehicleID, tt.req.ActorID = garageID, vehicleID, actorID

			if tt.findErr != nil {
				vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(nil, tt.findErr).Once()
			} else {
				vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
			}

			if tt.req.CustomerID != nil && *tt.req.CustomerID == newCustomerID {
				owner := &domain.Customer{ID: newCustomerID, FullName: "Le Chi"}
				if tt.ownerErr != nil {
					owner = nil
				}

				customers.EXPECT().FindByID(mock.Anything, garageID, newCustomerID).Return(owner, tt.ownerErr).Once()
			}

			if tt.check != nil || tt.updateErr != nil {
				vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.updateErr).Once()
			}

			v, err := uc.Update(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, actorID, *v.UpdatedBy)
			tt.check(t, v)
		})
	}
}

func TestVehicleUseCase_GetListDelete(t *testing.T) {
	t.Parallel()

	uc, vehicles, _ := newVehicleUseCase(t)

	vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
	vehicles.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrVehicleNotFound).Once()
	vehicles.EXPECT().List(mock.Anything, garageID, usecase.VehicleFilter{
		ListFilter: usecase.ListFilter{Query: "51F", Limit: 5, Offset: 5}, CustomerID: customerID,
	}).Return([]*domain.Vehicle{existingVehicle()}, 6, nil).Once()
	vehicles.EXPECT().List(mock.Anything, "garage-2", mock.Anything).Return(nil, 0, errBoom).Once()
	vehicles.EXPECT().SoftDelete(mock.Anything, garageID, vehicleID, actorID).Return(nil).Once()
	vehicles.EXPECT().SoftDelete(mock.Anything, garageID, "missing", actorID).Return(domain.ErrVehicleNotFound).Once()

	v, err := uc.Get(t.Context(), garageID, vehicleID)
	require.NoError(t, err)
	assert.Equal(t, vehicleID, v.ID)

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)

	page, err := uc.List(t.Context(), usecase.ListVehiclesRequest{
		PageRequest: usecase.PageRequest{Page: 2, PageSize: 5}, GarageID: garageID, CustomerID: " " + customerID, Query: "51F",
	})
	require.NoError(t, err)
	assert.Equal(t, 6, page.Total)
	assert.Equal(t, 2, page.Page)

	_, err = uc.List(t.Context(), usecase.ListVehiclesRequest{GarageID: "garage-2"})
	assertAppError(t, err, apperror.CodeInternal)

	require.NoError(t, uc.Delete(t.Context(), garageID, vehicleID, actorID))
	assertAppError(t, uc.Delete(t.Context(), garageID, "missing", actorID), apperror.CodeNotFound)
}

func TestVehicleUseCase_GetHistory(t *testing.T) {
	t.Parallel()

	t.Run("no history reader configured", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _ := newVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()

		entries, err := uc.GetHistory(t.Context(), garageID, vehicleID)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("unknown vehicle", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _ := newVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrVehicleNotFound).Once()

		_, err := uc.GetHistory(t.Context(), garageID, "missing")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("reads history when configured", func(t *testing.T) {
		t.Parallel()

		vehicles := mocks.NewMockVehicleRepository(t)
		customers := mocks.NewMockCustomerRepository(t)
		history := mocks.NewMockHistoryReader(t)
		uc := usecase.NewVehicleUseCase(vehicles, customers, history)

		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()

		want := []usecase.HistoryEntry{{Type: usecase.HistoryEntryQuote, EntityID: "q-1", Number: "BG-1"}}
		history.EXPECT().History(mock.Anything, garageID, vehicleID).Return(want, nil).Once()

		entries, err := uc.GetHistory(t.Context(), garageID, vehicleID)
		require.NoError(t, err)
		assert.Equal(t, want, entries)

		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		history.EXPECT().History(mock.Anything, garageID, vehicleID).Return(nil, errBoom).Once()

		_, err = uc.GetHistory(t.Context(), garageID, vehicleID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
