package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestVehicleUseCase_EditLock(t *testing.T) {
	t.Parallel()

	t.Run("an identity field is locked while a live document refers to the vehicle", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		usage.EXPECT().VehicleInUse(mock.Anything, garageID, vehicleID).Return(true, nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, LicensePlate: new("30A-999.99"),
		})
		require.Error(t, err)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assert.Equal(t, domain.ReasonVehicleFieldLocked, apperror.ReasonOf(err))
	})

	t.Run("an unchanged value is not an edit", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, []string{vehicleID}).Return(map[string]bool{vehicleID: true}, nil).Once()

		v, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, LicensePlate: new("51f-123.45"), Make: new("Toyota"),
		})
		require.NoError(t, err)
		assert.False(t, v.Editable)
		assert.Len(t, v.LockedFields(), len(domain.IdentityFields))
	})

	t.Run("a vehicle on no document is editable and free to change", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		usage.EXPECT().VehicleInUse(mock.Anything, garageID, vehicleID).Return(false, nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, []string{vehicleID}).Return(map[string]bool{}, nil).Once()

		v, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, LicensePlate: new("30A-999.99"),
		})
		require.NoError(t, err)
		assert.True(t, v.Editable)
		assert.Empty(t, v.LockedFields())
	})

	t.Run("colour-like fields never lock: only identity fields do", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		usage.EXPECT().VehicleInUse(mock.Anything, garageID, vehicleID).Return(true, nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, Make: new("Honda"),
		})
		assert.Equal(t, domain.ReasonVehicleFieldLocked, apperror.ReasonOf(err))
	})

	t.Run("the owner is not changed by an update", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, _, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, CustomerID: new(newCustomerID),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("usage check failures", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Twice()
		usage.EXPECT().VehicleInUse(mock.Anything, garageID, vehicleID).Return(false, errBoom).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateVehicleRequest{GarageID: garageID, VehicleID: vehicleID, VIN: new("X")})
		assertAppError(t, err, apperror.CodeInternal)

		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, []string{vehicleID}).Return(nil, errBoom).Once()

		_, err = uc.Get(t.Context(), garageID, vehicleID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestVehicleUseCase_Correct(t *testing.T) {
	t.Parallel()

	correct := func(field domain.IdentityField, value string) usecase.CorrectVehicleRequest {
		return usecase.CorrectVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, ActorID: actorID, Field: field, Value: value, Reason: " nhập nhầm biển số ",
		}
	}

	t.Run("changes a locked field and audits who, what and why", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, usage, rec := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, []string{vehicleID}).Return(map[string]bool{vehicleID: true}, nil).Once()

		v, err := uc.Correct(t.Context(), correct(domain.FieldLicensePlate, "30a 999.99"))
		require.NoError(t, err)
		assert.Equal(t, "30A 999.99", v.LicensePlate)
		assert.False(t, v.Editable, "still on a live document")

		require.Len(t, rec.entries, 1)
		entry := rec.entries[0]
		assert.Equal(t, usecase.ActionVehicleCorrected, entry.Action)
		assert.Equal(t, vehicleID, entry.EntityID)
		assert.Equal(t, actorID, entry.Actor)

		before, ok := entry.Before.(map[string]any)
		require.True(t, ok)
		after, ok := entry.After.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "51F-123.45", before["license_plate"])
		assert.Equal(t, "30A 999.99", after["license_plate"])
		assert.Equal(t, "nhập nhầm biển số", after["reason"])
	})

	t.Run("every identity field", func(t *testing.T) {
		t.Parallel()

		for field, value := range map[domain.IdentityField]string{
			domain.FieldVIN: "vin123", domain.FieldEngineNumber: "en-9", domain.FieldMake: "Honda", domain.FieldModel: "City",
			domain.FieldYear: "2021",
		} {
			uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
			vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
			vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
			usage.EXPECT().VehiclesInUse(mock.Anything, garageID, mock.Anything).Return(map[string]bool{}, nil).Once()

			v, err := uc.Correct(t.Context(), correct(field, value))
			require.NoError(t, err, field)
			assert.True(t, v.Editable)
		}

		// A blank year clears it.
		uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, mock.Anything).Return(map[string]bool{}, nil).Once()

		v, err := uc.Correct(t.Context(), correct(domain.FieldYear, ""))
		require.NoError(t, err)
		assert.Nil(t, v.Year)
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, _, _ := newLockableVehicleUseCase(t)

		noReason := correct(domain.FieldVIN, "x")
		noReason.Reason = "  "
		_, err := uc.Correct(t.Context(), noReason)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = uc.Correct(t.Context(), correct("colour", "red"))
		assertAppError(t, err, apperror.CodeInvalidArgument)

		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Times(3)

		_, err = uc.Correct(t.Context(), correct(domain.FieldYear, "abc"))
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = uc.Correct(t.Context(), correct(domain.FieldYear, "1500"))
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = uc.Correct(t.Context(), correct(domain.FieldLicensePlate, "!!"))
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, _, _, rec := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(nil, domain.ErrVehicleNotFound).Once()

		_, err := uc.Correct(t.Context(), correct(domain.FieldVIN, "x"))
		assertAppError(t, err, apperror.CodeNotFound)

		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Twice()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(domain.ErrLicensePlateTaken).Once()

		_, err = uc.Correct(t.Context(), correct(domain.FieldLicensePlate, "30A-999.99"))
		assertAppError(t, err, apperror.CodeAlreadyExists)

		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()

		rec.err = errBoom

		_, err = uc.Correct(t.Context(), correct(domain.FieldVIN, "x"))
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestVehicleUseCase_ChangeOwner(t *testing.T) {
	t.Parallel()

	req := usecase.ChangeVehicleOwnerRequest{
		GarageID: garageID, VehicleID: vehicleID, ActorID: actorID, CustomerID: newCustomerID, Reason: "đã bán xe",
	}

	t.Run("moves the vehicle and audits it", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, customers, usage, rec := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Once()
		customers.EXPECT().FindByID(mock.Anything, garageID, newCustomerID).
			Return(&domain.Customer{ID: newCustomerID, FullName: "Le Hoa"}, nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
		usage.EXPECT().VehiclesInUse(mock.Anything, garageID, mock.Anything).Return(map[string]bool{}, nil).Once()

		v, err := uc.ChangeOwner(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, newCustomerID, v.CustomerID)
		assert.Equal(t, "Le Hoa", v.CustomerName)

		require.Len(t, rec.entries, 1)
		assert.Equal(t, usecase.ActionVehicleOwnerChanged, rec.entries[0].Action)

		before, ok := rec.entries[0].Before.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, customerID, before["customer_id"])
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()

		uc, vehicles, customers, _, rec := newLockableVehicleUseCase(t)
		vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(existingVehicle(), nil).Times(3)

		same := req
		same.CustomerID = customerID
		_, err := uc.ChangeOwner(t.Context(), same)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		customers.EXPECT().FindByID(mock.Anything, garageID, newCustomerID).Return(nil, domain.ErrCustomerNotFound).Once()

		_, err = uc.ChangeOwner(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		customers.EXPECT().FindByID(mock.Anything, garageID, newCustomerID).Return(&domain.Customer{ID: newCustomerID}, nil).Once()
		vehicles.EXPECT().Update(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.ChangeOwner(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		assert.Empty(t, rec.entries)

		vehicles.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrVehicleNotFound).Once()

		missing := req
		missing.VehicleID = "missing"
		_, err = uc.ChangeOwner(t.Context(), missing)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestVehicleUseCase_ListMarksTheLockedOnes(t *testing.T) {
	t.Parallel()

	uc, vehicles, _, usage, _ := newLockableVehicleUseCase(t)
	other := existingVehicle()
	other.ID = "33333333-3333-3333-3333-333333333339"

	vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return([]*domain.Vehicle{existingVehicle(), other}, 2, nil).Once()
	usage.EXPECT().VehiclesInUse(mock.Anything, garageID, []string{vehicleID, other.ID}).
		Return(map[string]bool{vehicleID: true}, nil).Once()

	page, err := uc.List(t.Context(), usecase.ListVehiclesRequest{GarageID: garageID})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.False(t, page.Items[0].Editable)
	assert.True(t, page.Items[1].Editable)
}
