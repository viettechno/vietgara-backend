package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	supplierID = "44444444-4444-4444-4444-444444444444"
	actorID    = "55555555-5555-5555-5555-555555555555"
)

var errBoom = assert.AnError

func assertAppError(t *testing.T, err error, want apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected *apperror.AppError, got %v", err)
	assert.Equal(t, want, appErr.Code)
}

func existingSupplier() *domain.Supplier {
	return &domain.Supplier{
		ID: supplierID, GarageID: garageID, Name: "Phu Tung Sai Gon",
		Contact: domain.Contact{ContactPerson: new("Ms. Lan")},
		Bank:    domain.BankDetails{BankName: new("Vietcombank")},
	}
}

func TestUseCase_Create(t *testing.T) {
	t.Parallel()

	valid := func() usecase.CreateRequest {
		return usecase.CreateRequest{
			GarageID: garageID, ActorID: actorID, Name: " Phu Tung Sai Gon ", TaxCode: new("01-001 abc"),
			Contact: usecase.ContactInput{
				ContactPerson: new(" Mr. Minh "), Phone: new("+84 28 1234 5678"), Email: new("Sales@PhuTung.vn"), Address: new(""),
			},
			Bank: usecase.BankInput{BankName: new(" Vietcombank "), AccountNumber: new("0011 2233"), AccountHolder: new("")},
			Note: new("  "),
		}
	}

	tests := []struct {
		name      string
		mutate    func(r *usecase.CreateRequest)
		createErr error
		wantCode  apperror.Code
	}{
		{name: "normalizes and stores the supplier"},
		{name: "requires a name", mutate: func(r *usecase.CreateRequest) { r.Name = "" }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long contact person", mutate: func(r *usecase.CreateRequest) {
			r.Contact.ContactPerson = new(string(make([]rune, 256)))
		}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad phone", mutate: func(r *usecase.CreateRequest) { r.Contact.Phone = new("abc") }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad email", mutate: func(r *usecase.CreateRequest) { r.Contact.Email = new("sales") }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long tax code", mutate: func(r *usecase.CreateRequest) { r.TaxCode = new("0123456789012345678901") }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long bank name", mutate: func(r *usecase.CreateRequest) {
			r.Bank.BankName = new(string(make([]rune, 101)))
		}, wantCode: apperror.CodeInvalidArgument},
		{name: "taken name", createErr: domain.ErrSupplierNameTaken, wantCode: apperror.CodeAlreadyExists},
		{name: "repository failure", createErr: errBoom, wantCode: apperror.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			suppliers := mocks.NewMockSupplierRepository(t)
			uc := usecase.NewUseCase(suppliers)

			req := valid()
			if tt.mutate != nil {
				tt.mutate(&req)
			}

			if tt.wantCode == "" || tt.createErr != nil {
				suppliers.EXPECT().Create(mock.Anything, mock.Anything).Return(tt.createErr).Once()
			}

			s, err := uc.Create(t.Context(), req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "Phu Tung Sai Gon", s.Name)
			assert.Equal(t, "01-001 ABC", *s.TaxCode)
			assert.Equal(t, "Mr. Minh", *s.Contact.ContactPerson)
			assert.Equal(t, "+842812345678", *s.Contact.Phone)
			assert.Equal(t, "sales@phutung.vn", *s.Contact.Email)
			assert.Nil(t, s.Contact.Address)
			assert.Equal(t, "Vietcombank", *s.Bank.BankName)
			assert.Equal(t, "0011 2233", *s.Bank.AccountNumber)
			assert.Nil(t, s.Bank.AccountHolder)
			assert.Nil(t, s.Note)
			assert.Equal(t, actorID, *s.CreatedBy)
		})
	}
}

func TestUseCase_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       usecase.UpdateRequest
		findErr   error
		updateErr error
		wantCode  apperror.Code
	}{
		{name: "replaces contact and bank and sets a note", req: usecase.UpdateRequest{
			Contact: &usecase.ContactInput{Phone: new("0909123456")},
			Bank:    &usecase.BankInput{AccountNumber: new("123")},
			Note:    new("VIP"),
		}},
		{name: "unknown supplier", findErr: domain.ErrSupplierNotFound, wantCode: apperror.CodeNotFound},
		{name: "blank name", req: usecase.UpdateRequest{Name: new(" ")}, wantCode: apperror.CodeInvalidArgument},
		{name: "invalid contact", req: usecase.UpdateRequest{Contact: &usecase.ContactInput{Email: new("x")}}, wantCode: apperror.CodeInvalidArgument},
		{name: "invalid bank", req: usecase.UpdateRequest{Bank: &usecase.BankInput{BankName: new(string(make([]rune, 101)))}}, wantCode: apperror.CodeInvalidArgument},
		{name: "long note", req: usecase.UpdateRequest{Note: new(string(make([]rune, 2001)))}, wantCode: apperror.CodeInvalidArgument},
		{name: "taken name", req: usecase.UpdateRequest{Name: new("Lop Xe Viet")}, updateErr: domain.ErrSupplierNameTaken, wantCode: apperror.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			suppliers := mocks.NewMockSupplierRepository(t)
			uc := usecase.NewUseCase(suppliers)
			tt.req.GarageID, tt.req.SupplierID, tt.req.ActorID = garageID, supplierID, actorID

			if tt.findErr != nil {
				suppliers.EXPECT().FindByID(mock.Anything, garageID, supplierID).Return(nil, tt.findErr).Once()
			} else {
				suppliers.EXPECT().FindByID(mock.Anything, garageID, supplierID).Return(existingSupplier(), nil).Once()
			}

			if tt.wantCode == "" || tt.updateErr != nil {
				suppliers.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.updateErr).Once()
			}

			s, err := uc.Update(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Nil(t, s.Contact.ContactPerson, "a new contact replaces the old one")
			assert.Equal(t, "+84909123456", *s.Contact.Phone)
			assert.Nil(t, s.Bank.BankName, "a new bank block replaces the old one")
			assert.Equal(t, "123", *s.Bank.AccountNumber)
			assert.Equal(t, "VIP", *s.Note)
			assert.Equal(t, actorID, *s.UpdatedBy)
		})
	}
}

func TestUseCase_GetListDelete(t *testing.T) {
	t.Parallel()

	suppliers := mocks.NewMockSupplierRepository(t)
	uc := usecase.NewUseCase(suppliers)

	suppliers.EXPECT().FindByID(mock.Anything, garageID, supplierID).Return(existingSupplier(), nil).Once()
	suppliers.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSupplierNotFound).Once()
	suppliers.EXPECT().List(mock.Anything, garageID, usecase.ListFilter{Query: "sai gon", Limit: 20}).
		Return([]*domain.Supplier{existingSupplier()}, 1, nil).Once()
	suppliers.EXPECT().List(mock.Anything, "garage-2", mock.Anything).Return(nil, 0, errBoom).Once()
	suppliers.EXPECT().SoftDelete(mock.Anything, garageID, supplierID, actorID).Return(nil).Once()
	suppliers.EXPECT().SoftDelete(mock.Anything, garageID, "missing", actorID).Return(domain.ErrSupplierNotFound).Once()

	s, err := uc.Get(t.Context(), garageID, supplierID)
	require.NoError(t, err)
	assert.Equal(t, supplierID, s.ID)

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)

	page, err := uc.List(t.Context(), usecase.ListRequest{GarageID: garageID, Query: " sai gon "})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total)
	assert.Equal(t, 20, page.PageSize)

	_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: "garage-2"})
	assertAppError(t, err, apperror.CodeInternal)

	require.NoError(t, uc.Delete(t.Context(), garageID, supplierID, actorID))
	assertAppError(t, uc.Delete(t.Context(), garageID, "missing", actorID), apperror.CodeNotFound)
}
