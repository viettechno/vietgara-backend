package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/internal/supplier/handler"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
)

const garageID = "11111111-1111-4111-8111-111111111111"

var anyArg = mock.Anything

type env struct {
	srv       *handler.Server
	suppliers *mocks.MockSupplierRepository
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{suppliers: mocks.NewMockSupplierRepository(t)}
	e.srv = handler.NewServer(usecase.NewUseCase(e.suppliers))

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = "22222222-2222-4222-8222-222222222222"

	return middleware.WithClaims(t.Context(), claims)
}

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

func existingSupplier() *domain.Supplier {
	person, bank := "Anh Ba", "ACB"

	return &domain.Supplier{
		ID: "s-1", GarageID: garageID, Name: "Phụ tùng A",
		Contact: domain.Contact{ContactPerson: &person}, Bank: domain.BankDetails{BankName: &bank},
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 5)
	assert.Equal(t, access.SupplierRead, p[supplierv1.SupplierService_ListSuppliers_FullMethodName].Garage.Permission)
	assert.Equal(t, access.SupplierWrite, p[supplierv1.SupplierService_CreateSupplier_FullMethodName].Garage.Permission)
}

func TestSupplierRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.suppliers.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	created, err := e.srv.CreateSupplier(ctx, &supplierv1.CreateSupplierRequest{GarageId: garageID, Supplier: &supplierv1.Supplier{
		Name: "Bảo Dưỡng Xe", Bank: &commonv1.BankDetails{AccountNumber: new("0011")},
	}})
	require.NoError(t, err)
	assert.Equal(t, "Bảo Dưỡng Xe", created.GetSupplier().GetName())
	assert.Equal(t, "0011", created.GetSupplier().GetBank().GetAccountNumber())

	_, err = e.srv.CreateSupplier(ctx, &supplierv1.CreateSupplierRequest{GarageId: garageID, Supplier: &supplierv1.Supplier{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	// A fresh supplier per lookup: the use case mutates what it loads.
	e.suppliers.EXPECT().FindByID(anyArg, garageID, "s-1").
		RunAndReturn(func(context.Context, string, string) (*domain.Supplier, error) { return existingSupplier(), nil })

	got, err := e.srv.GetSupplier(ctx, &supplierv1.GetSupplierRequest{GarageId: garageID, SupplierId: "s-1"})
	require.NoError(t, err)
	assert.Equal(t, "Anh Ba", got.GetSupplier().GetContact().GetContactPerson())
	assert.Equal(t, "ACB", got.GetSupplier().GetBank().GetBankName())

	e.suppliers.EXPECT().Update(anyArg, anyArg).Return(nil)

	tax, note, phone := "0301", "Net 30", "0909123456"
	updated, err := e.srv.UpdateSupplier(ctx, &supplierv1.UpdateSupplierRequest{
		GarageId: garageID, SupplierId: "s-1",
		Supplier: &supplierv1.Supplier{
			Name: "B", TaxCode: &tax, Note: &note,
			Contact: &supplierv1.SupplierContact{Phone: &phone},
			Bank:    &commonv1.BankDetails{AccountHolder: new("CTY B")},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "B", updated.GetSupplier().GetName())
	assert.Equal(t, "+84909123456", updated.GetSupplier().GetContact().GetPhone())
	assert.Empty(t, updated.GetSupplier().GetContact().GetContactPerson(), "the contact block is replaced")
	assert.Empty(t, updated.GetSupplier().GetBank().GetBankName(), "the bank block is replaced")
	assert.Equal(t, "CTY B", updated.GetSupplier().GetBank().GetAccountHolder())

	updated, err = e.srv.UpdateSupplier(ctx, &supplierv1.UpdateSupplierRequest{
		GarageId: garageID, SupplierId: "s-1",
		Supplier:   &supplierv1.Supplier{Bank: &commonv1.BankDetails{BankName: new("VCB")}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"bank.bank_name", "bank.account_number"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "VCB", updated.GetSupplier().GetBank().GetBankName())
	assert.Equal(t, "Anh Ba", updated.GetSupplier().GetContact().GetContactPerson(), "unnamed fields are kept")

	_, err = e.srv.UpdateSupplier(ctx, &supplierv1.UpdateSupplierRequest{
		GarageId: garageID, SupplierId: "s-1", Supplier: &supplierv1.Supplier{},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"garage_id"}},
	})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	e.suppliers.EXPECT().List(anyArg, garageID, anyArg).Return([]*domain.Supplier{existingSupplier()}, 1, nil).Once()

	list, err := e.srv.ListSuppliers(ctx, &supplierv1.ListSuppliersRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
	assert.Equal(t, int32(1), list.GetPagination().GetTotal())

	e.suppliers.EXPECT().List(anyArg, garageID, anyArg).Return(nil, 0, assert.AnError).Once()

	_, err = e.srv.ListSuppliers(ctx, &supplierv1.ListSuppliersRequest{GarageId: garageID})
	require.Error(t, err)

	e.suppliers.EXPECT().SoftDelete(anyArg, garageID, "s-1", anyArg).Return(nil).Once()

	_, err = e.srv.DeleteSupplier(ctx, &supplierv1.DeleteSupplierRequest{GarageId: garageID, SupplierId: "s-1"})
	require.NoError(t, err)

	e.suppliers.EXPECT().SoftDelete(anyArg, garageID, "s-2", anyArg).Return(domain.ErrSupplierNotFound).Once()

	_, err = e.srv.DeleteSupplier(ctx, &supplierv1.DeleteSupplierRequest{GarageId: garageID, SupplierId: "s-2"})
	assert.Equal(t, "NOT_FOUND", reason(err))
}
