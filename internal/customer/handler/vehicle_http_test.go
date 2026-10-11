package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

func existingVehicle() *domain.Vehicle {
	year, mk := 2020, "Toyota"

	return &domain.Vehicle{ID: "v-1", GarageID: garageID, CustomerID: "c-1", CustomerName: "Khách", LicensePlate: "51A-123.45", Year: &year, Make: &mk}
}

func TestVehicleRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.customers.EXPECT().FindByID(anyArg, garageID, "c-1").Return(existingCustomer(), nil)
	e.vehicles.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	year := int32(2020)
	created, err := e.srv.CreateVehicle(ctx, &customerv1.CreateVehicleRequest{GarageId: garageID, Vehicle: &customerv1.Vehicle{CustomerId: "c-1", LicensePlate: "51a12345", Year: &year}})
	require.NoError(t, err)
	assert.EqualValues(t, 2020, created.GetVehicle().GetYear())

	_, err = e.srv.CreateVehicle(ctx, &customerv1.CreateVehicleRequest{GarageId: garageID, Vehicle: &customerv1.Vehicle{CustomerId: "c-1"}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-1").Return(existingVehicle(), nil)
	got, err := e.srv.GetVehicle(ctx, &customerv1.GetVehicleRequest{GarageId: garageID, VehicleId: "v-1"})
	require.NoError(t, err)
	assert.Equal(t, "Toyota", got.GetVehicle().GetMake())

	e.vehicles.EXPECT().Update(anyArg, anyArg).Return(nil)
	e.usage.EXPECT().VehicleInUse(anyArg, garageID, "v-1").Return(false, nil).Once()

	model, vin, engine := "Vios", "VIN123", "E1"
	updated, err := e.srv.UpdateVehicle(ctx, &customerv1.UpdateVehicleRequest{
		GarageId: garageID, VehicleId: "v-1",
		Vehicle: &customerv1.Vehicle{CustomerId: "c-1", LicensePlate: "51A-999.99", Model: &model, Vin: &vin, EngineNumber: &engine},
	})
	require.NoError(t, err)
	assert.Equal(t, "Vios", updated.GetVehicle().GetModel())
	assert.Nil(t, updated.GetVehicle().Year, "0 year clears it")

	_, err = e.srv.UpdateVehicle(ctx, &customerv1.UpdateVehicleRequest{GarageId: garageID, VehicleId: "v-1", Vehicle: &customerv1.Vehicle{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	e.vehicles.EXPECT().List(anyArg, garageID, anyArg).Return([]*domain.Vehicle{existingVehicle()}, 1, nil).Once()
	list, err := e.srv.ListVehicles(ctx, &customerv1.ListVehiclesRequest{GarageId: garageID, CustomerId: "c-1"})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	e.vehicles.EXPECT().List(anyArg, garageID, anyArg).Return(nil, 0, assert.AnError).Once()
	_, err = e.srv.ListVehicles(ctx, &customerv1.ListVehiclesRequest{GarageId: garageID})
	require.Error(t, err)

	e.usage.EXPECT().VehicleInUse(anyArg, garageID, "v-1").Return(false, nil).Once()
	e.vehicles.EXPECT().SoftDelete(anyArg, garageID, "v-1", anyArg).Return(nil).Once()

	deleted, err := e.srv.DeleteVehicle(ctx, &customerv1.DeleteVehicleRequest{GarageId: garageID, VehicleId: "v-1"})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, deleted.GetOutcome())

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-4").Return(existingVehicle(), nil).Once()
	e.usage.EXPECT().VehicleInUse(anyArg, garageID, "v-4").Return(true, nil).Once()

	dry, err := e.srv.DeleteVehicle(ctx, &customerv1.DeleteVehicleRequest{GarageId: garageID, VehicleId: "v-4", DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, dry.GetOutcome())

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-2").Return(nil, domain.ErrVehicleNotFound).Once()
	_, err = e.srv.DeleteVehicle(ctx, &customerv1.DeleteVehicleRequest{GarageId: garageID, VehicleId: "v-2"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	e.vehicles.EXPECT().SetActive(anyArg, garageID, "v-4", true, anyArg).Return(nil).Once()
	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-4").Return(existingVehicle(), nil).Once()

	activated, err := e.srv.ActivateVehicle(ctx, &customerv1.ActivateVehicleRequest{GarageId: garageID, VehicleId: "v-4"})
	require.NoError(t, err)
	assert.NotEmpty(t, activated.GetVehicle().GetId())

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-3").Return(nil, domain.ErrVehicleNotFound)
	_, err = e.srv.GetVehicle(ctx, &customerv1.GetVehicleRequest{GarageId: garageID, VehicleId: "v-3"})
	assert.Equal(t, "NOT_FOUND", reason(err))
	_, err = e.srv.UpdateVehicle(ctx, &customerv1.UpdateVehicleRequest{GarageId: garageID, VehicleId: "v-3", Vehicle: &customerv1.Vehicle{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"make"}}})
	assert.Equal(t, "NOT_FOUND", reason(err))
}

func TestCorrectVehicleAndChangeOwner(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-1").Return(existingVehicle(), nil).Twice()
	e.vehicles.EXPECT().Update(anyArg, anyArg).Return(nil).Twice()

	corrected, err := e.srv.CorrectVehicle(ctx, &customerv1.CorrectVehicleRequest{
		GarageId: garageID, VehicleId: "v-1", Field: customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_VIN,
		Value: "abc123", Reason: "typo",
	})
	require.NoError(t, err)
	assert.Equal(t, "ABC123", corrected.GetVehicle().GetVin())
	assert.True(t, corrected.GetVehicle().GetEditable())
	assert.Empty(t, corrected.GetVehicle().GetLockedFields())

	_, err = e.srv.CorrectVehicle(ctx, &customerv1.CorrectVehicleRequest{GarageId: garageID, VehicleId: "v-1", Value: "x", Reason: "r"})
	assert.Equal(t, "VALIDATION_ERROR", reason(err), "a field is required")

	e.customers.EXPECT().FindByID(anyArg, garageID, "c-2").Return(&domain.Customer{ID: "c-2", FullName: "Le Hoa"}, nil).Once()

	moved, err := e.srv.ChangeVehicleOwner(ctx, &customerv1.ChangeVehicleOwnerRequest{
		GarageId: garageID, VehicleId: "v-1", CustomerId: "c-2", Reason: "sold",
	})
	require.NoError(t, err)
	assert.Equal(t, "c-2", moved.GetVehicle().GetCustomerId())
	assert.Equal(t, "Le Hoa", moved.GetVehicle().GetCustomerName())
}

func TestLockedVehicleIsMappedToItsFields(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.usage.ExpectedCalls = nil
	e.usage.EXPECT().VehiclesInUse(anyArg, garageID, []string{"v-1"}).Return(map[string]bool{"v-1": true}, nil).Once()
	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-1").Return(existingVehicle(), nil).Once()

	got, err := e.srv.GetVehicle(signedIn(t), &customerv1.GetVehicleRequest{GarageId: garageID, VehicleId: "v-1"})
	require.NoError(t, err)
	assert.False(t, got.GetVehicle().GetEditable())
	assert.ElementsMatch(t, []customerv1.VehicleIdentityField{
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_LICENSE_PLATE,
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_VIN,
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_ENGINE_NUMBER,
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_MAKE,
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_MODEL,
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_YEAR,
	}, got.GetVehicle().GetLockedFields())
}
