package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

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

	e.vehicles.EXPECT().SoftDelete(anyArg, garageID, "v-1", anyArg).Return(nil).Once()
	_, err = e.srv.DeleteVehicle(ctx, &customerv1.DeleteVehicleRequest{GarageId: garageID, VehicleId: "v-1"})
	require.NoError(t, err)

	e.vehicles.EXPECT().SoftDelete(anyArg, garageID, "v-2", anyArg).Return(domain.ErrVehicleNotFound).Once()
	_, err = e.srv.DeleteVehicle(ctx, &customerv1.DeleteVehicleRequest{GarageId: garageID, VehicleId: "v-2"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	e.vehicles.EXPECT().FindByID(anyArg, garageID, "v-3").Return(nil, domain.ErrVehicleNotFound)
	_, err = e.srv.GetVehicle(ctx, &customerv1.GetVehicleRequest{GarageId: garageID, VehicleId: "v-3"})
	assert.Equal(t, "NOT_FOUND", reason(err))
	_, err = e.srv.UpdateVehicle(ctx, &customerv1.UpdateVehicleRequest{GarageId: garageID, VehicleId: "v-3", Vehicle: &customerv1.Vehicle{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"make"}}})
	assert.Equal(t, "NOT_FOUND", reason(err))
}
