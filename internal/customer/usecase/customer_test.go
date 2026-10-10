package usecase_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID   = "garage-1"
	actorID    = "user-1"
	customerID = "11111111-1111-1111-1111-111111111111"
)

var (
	fixedNow = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	errBoom  = errors.New("boom")
)

func clock() usecase.Option {
	return usecase.WithClock(func() time.Time { return fixedNow })
}

func assertAppError(t *testing.T, err error, want apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected *apperror.AppError, got %v", err)
	assert.Equal(t, want, appErr.Code)
}

func existingCustomer() *domain.Customer {
	phone := "0912345678"

	return &domain.Customer{
		ID: customerID, GarageID: garageID, FullName: "Tran Binh", Phone: &phone, VehicleCount: 2,
		ConsentLog: []domain.ConsentRecord{{Granted: true, RecordedAt: fixedNow.Add(-time.Hour)}},
	}
}

func TestCustomerUseCase_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       usecase.CreateCustomerRequest
		createErr error
		wantCode  apperror.Code
		check     func(t *testing.T, c *domain.Customer)
	}{
		{
			name: "normalizes fields and records consent",
			req: usecase.CreateCustomerRequest{
				GarageID: garageID, ActorID: actorID, FullName: "  Tran Binh ", Phone: new("+84 912 345 678"),
				Email: new(" Binh@Example.com "), Address: new("   "), PersonalDataConsent: true,
			},
			check: func(t *testing.T, c *domain.Customer) {
				t.Helper()
				assert.Equal(t, "Tran Binh", c.FullName)
				assert.Equal(t, "0912345678", *c.Phone)
				assert.Equal(t, "binh@example.com", *c.Email)
				assert.Nil(t, c.Address, "blank optional fields are stored as NULL")
				assert.Equal(t, actorID, *c.CreatedBy)
				assert.Equal(t, []domain.ConsentRecord{{Granted: true, RecordedAt: fixedNow, RecordedBy: actorID}}, c.ConsentLog)
			},
		},
		{
			name: "records a refused consent without an actor",
			req:  usecase.CreateCustomerRequest{GarageID: garageID, FullName: "Binh"},
			check: func(t *testing.T, c *domain.Customer) {
				t.Helper()
				assert.Nil(t, c.CreatedBy)
				require.Len(t, c.ConsentLog, 1)
				assert.False(t, c.ConsentLog[0].Granted)
			},
		},
		{name: "requires a name", req: usecase.CreateCustomerRequest{FullName: "  "}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad phone", req: usecase.CreateCustomerRequest{FullName: "A", Phone: new("12")}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad email", req: usecase.CreateCustomerRequest{FullName: "A", Email: new("x@")}, wantCode: apperror.CodeInvalidArgument},
		{
			name: "rejects a long address", req: usecase.CreateCustomerRequest{FullName: "A", Address: new(strings.Repeat("a", 501))},
			wantCode: apperror.CodeInvalidArgument,
		},
		{name: "maps a taken phone", req: usecase.CreateCustomerRequest{FullName: "A"}, createErr: domain.ErrPhoneTaken, wantCode: apperror.CodeAlreadyExists},
		{name: "wraps storage failures", req: usecase.CreateCustomerRequest{FullName: "A"}, createErr: errBoom, wantCode: apperror.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			customers := mocks.NewMockCustomerRepository(t)
			uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), clock())

			if tt.wantCode == "" || tt.createErr != nil {
				customers.EXPECT().Create(mock.Anything, mock.Anything).Return(tt.createErr).Once()
			}

			c, err := uc.Create(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, c.ID)
			tt.check(t, c)
		})
	}
}

func TestCustomerUseCase_Get(t *testing.T) {
	t.Parallel()

	customers := mocks.NewMockCustomerRepository(t)
	uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t))

	customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
	customers.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrCustomerNotFound).Once()

	c, err := uc.Get(t.Context(), garageID, customerID)
	require.NoError(t, err)
	assert.Equal(t, "Tran Binh", c.FullName)

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestCustomerUseCase_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       usecase.UpdateCustomerRequest
		findErr   error
		updateErr error
		wantCode  apperror.Code
		check     func(t *testing.T, c *domain.Customer)
	}{
		{
			name: "applies only the given fields and clears blank ones",
			req:  usecase.UpdateCustomerRequest{FullName: new("Tran Van Binh"), Phone: new(""), ActorID: actorID},
			check: func(t *testing.T, c *domain.Customer) {
				t.Helper()
				assert.Equal(t, "Tran Van Binh", c.FullName)
				assert.Nil(t, c.Phone)
				assert.Equal(t, actorID, *c.UpdatedBy)
				assert.Len(t, c.ConsentLog, 1)
			},
		},
		{
			name: "records a withdrawn consent",
			req:  usecase.UpdateCustomerRequest{PersonalDataConsent: new(false), ActorID: actorID},
			check: func(t *testing.T, c *domain.Customer) {
				t.Helper()
				require.Len(t, c.ConsentLog, 2)
				assert.Equal(t, domain.ConsentRecord{Granted: false, RecordedAt: fixedNow, RecordedBy: actorID}, c.ConsentLog[1])
			},
		},
		{name: "unknown customer", findErr: domain.ErrCustomerNotFound, wantCode: apperror.CodeNotFound},
		{name: "invalid field", req: usecase.UpdateCustomerRequest{FullName: new("")}, wantCode: apperror.CodeInvalidArgument},
		{name: "taken phone", req: usecase.UpdateCustomerRequest{Phone: new("0987654321")}, updateErr: domain.ErrPhoneTaken, wantCode: apperror.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			customers := mocks.NewMockCustomerRepository(t)
			uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), clock())
			tt.req.GarageID, tt.req.CustomerID = garageID, customerID

			if tt.findErr != nil {
				customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(nil, tt.findErr).Once()
			} else {
				customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
			}

			if tt.check != nil || tt.updateErr != nil {
				customers.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.updateErr).Once()
			}

			c, err := uc.Update(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			tt.check(t, c)
		})
	}
}

func TestCustomerUseCase_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       usecase.PageRequest
		wantFilter usecase.ListFilter
		wantPage   int
		wantSize   int
	}{
		{name: "defaults", wantFilter: usecase.ListFilter{Query: "binh", Limit: 20}, wantPage: 1, wantSize: 20},
		{name: "offset from page", page: usecase.PageRequest{Page: 3, PageSize: 10}, wantFilter: usecase.ListFilter{Query: "binh", Limit: 10, Offset: 20}, wantPage: 3, wantSize: 10},
		{name: "caps the page size", page: usecase.PageRequest{Page: -1, PageSize: 500}, wantFilter: usecase.ListFilter{Query: "binh", Limit: 100}, wantPage: 1, wantSize: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			customers := mocks.NewMockCustomerRepository(t)
			uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t))
			customers.EXPECT().List(mock.Anything, garageID, tt.wantFilter).Return([]*domain.Customer{existingCustomer()}, 41, nil).Once()

			page, err := uc.List(t.Context(), usecase.ListCustomersRequest{PageRequest: tt.page, GarageID: garageID, Query: " binh "})
			require.NoError(t, err)
			assert.Equal(t, 41, page.Total)
			assert.Equal(t, tt.wantPage, page.Page)
			assert.Equal(t, tt.wantSize, page.PageSize)
			assert.Len(t, page.Items, 1)
		})
	}

	t.Run("wraps storage failures", func(t *testing.T) {
		t.Parallel()

		customers := mocks.NewMockCustomerRepository(t)
		uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t))
		customers.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.List(t.Context(), usecase.ListCustomersRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestCustomerUseCase_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deleteErr  error
		vehicleErr error
		wantCode   apperror.Code
	}{
		{name: "deletes the customer and their vehicles"},
		{name: "unknown customer", deleteErr: domain.ErrCustomerNotFound, wantCode: apperror.CodeNotFound},
		{name: "vehicle cleanup failure", vehicleErr: errBoom, wantCode: apperror.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			customers := mocks.NewMockCustomerRepository(t)
			vehicles := mocks.NewMockVehicleRepository(t)
			uc := usecase.NewCustomerUseCase(customers, vehicles)

			customers.EXPECT().SoftDelete(mock.Anything, garageID, customerID, actorID).Return(tt.deleteErr).Once()

			if tt.deleteErr == nil {
				vehicles.EXPECT().SoftDeleteByCustomer(mock.Anything, garageID, customerID, actorID).Return(tt.vehicleErr).Once()
			}

			err := uc.Delete(t.Context(), garageID, customerID, actorID)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
		})
	}
}
