package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 11)
	assert.Equal(t, access.CustomerWrite, p[customerv1.CustomerService_CreateCustomer_FullMethodName].Garage.Permission)
	assert.Equal(t, access.CustomerRead, p[customerv1.VehicleService_GetVehicleHistory_FullMethodName].Garage.Permission)
}

func existingCustomer() *domain.Customer {
	phone := "0901234567"

	return &domain.Customer{
		ID: "c-1", GarageID: garageID, FullName: "Khách", Phone: &phone, VehicleCount: 2,
		ConsentLog: []domain.ConsentRecord{{Granted: true, RecordedAt: time.Now()}},
	}
}

func TestCustomerRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.customers.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	phone := "0901 234 567"
	created, err := e.srv.CreateCustomer(ctx, &customerv1.CreateCustomerRequest{GarageId: garageID, Customer: &customerv1.Customer{FullName: "Khách", Phone: &phone, PersonalDataConsent: true}})
	require.NoError(t, err)
	assert.Equal(t, "+84901234567", created.GetCustomer().GetPhone())
	assert.True(t, created.GetCustomer().GetPersonalDataConsent())

	_, err = e.srv.CreateCustomer(ctx, &customerv1.CreateCustomerRequest{GarageId: garageID, Customer: &customerv1.Customer{}})
	assert.NotEmpty(t, reason(err))

	e.customers.EXPECT().FindByID(anyArg, garageID, "c-1").Return(existingCustomer(), nil).Times(3)
	got, err := e.srv.GetCustomer(ctx, &customerv1.GetCustomerRequest{GarageId: garageID, CustomerId: "c-1"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, got.GetCustomer().GetVehicleCount())

	e.customers.EXPECT().Update(anyArg, anyArg).Return(nil).Times(2)
	updated, err := e.srv.UpdateCustomer(ctx, &customerv1.UpdateCustomerRequest{GarageId: garageID, CustomerId: "c-1", Customer: &customerv1.Customer{FullName: "Mới"}})
	require.NoError(t, err)
	assert.Equal(t, "Mới", updated.GetCustomer().GetFullName())
	assert.Nil(t, updated.GetCustomer().Phone, "unset optional fields are cleared without a mask")

	email := "k@b.vn"
	updated, err = e.srv.UpdateCustomer(ctx, &customerv1.UpdateCustomerRequest{
		GarageId: garageID, CustomerId: "c-1", Customer: &customerv1.Customer{Email: &email, PersonalDataConsent: false},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email", "personal_data_consent", "address"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "k@b.vn", updated.GetCustomer().GetEmail())
	assert.False(t, updated.GetCustomer().GetPersonalDataConsent())

	_, err = e.srv.UpdateCustomer(ctx, &customerv1.UpdateCustomerRequest{GarageId: garageID, CustomerId: "c-1", Customer: &customerv1.Customer{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"garage_id"}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	e.customers.EXPECT().List(anyArg, garageID, anyArg).Return([]*domain.Customer{existingCustomer()}, 1, nil).Once()
	list, err := e.srv.ListCustomers(ctx, &customerv1.ListCustomersRequest{GarageId: garageID, Query: "kh", PageSize: 5})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 5, list.GetPagination().GetPageSize())

	e.customers.EXPECT().List(anyArg, garageID, anyArg).Return(nil, 0, assert.AnError).Once()
	_, err = e.srv.ListCustomers(ctx, &customerv1.ListCustomersRequest{GarageId: garageID})
	require.Error(t, err)

	e.customers.EXPECT().SoftDelete(anyArg, garageID, "c-1", anyArg).Return(nil).Once()
	e.vehicles.EXPECT().SoftDeleteByCustomer(anyArg, garageID, "c-1", anyArg).Return(nil).Once()
	_, err = e.srv.DeleteCustomer(ctx, &customerv1.DeleteCustomerRequest{GarageId: garageID, CustomerId: "c-1"})
	require.NoError(t, err)

	e.customers.EXPECT().SoftDelete(anyArg, garageID, "c-2", anyArg).Return(domain.ErrCustomerNotFound).Once()
	_, err = e.srv.DeleteCustomer(ctx, &customerv1.DeleteCustomerRequest{GarageId: garageID, CustomerId: "c-2"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	e.customers.EXPECT().FindByID(anyArg, garageID, "c-3").Return(nil, domain.ErrCustomerNotFound).Twice()
	_, err = e.srv.GetCustomer(ctx, &customerv1.GetCustomerRequest{GarageId: garageID, CustomerId: "c-3"})
	assert.Equal(t, "NOT_FOUND", reason(err))
	_, err = e.srv.UpdateCustomer(ctx, &customerv1.UpdateCustomerRequest{GarageId: garageID, CustomerId: "c-3", Customer: &customerv1.Customer{}})
	assert.Equal(t, "NOT_FOUND", reason(err))
}
