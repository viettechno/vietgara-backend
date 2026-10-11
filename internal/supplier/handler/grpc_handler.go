// Package handler exposes the Supplier module (FR-SUP-01) as a gRPC
// service, served as REST through grpc-gateway. Authorization happens in
// the request middleware from the policies below.
package handler

import (
	"context"

	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements supplierv1.SupplierServiceServer.
type Server struct {
	supplierv1.UnimplementedSupplierServiceServer

	suppliers *usecase.UseCase
}

// NewServer wires the handler to its use case.
func NewServer(suppliers *usecase.UseCase) *Server {
	return &Server{suppliers: suppliers}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.SupplierRead), access.Requires(access.SupplierWrite)

	return middleware.Policies{
		supplierv1.SupplierService_ListSuppliers_FullMethodName:    middleware.GarageMethod(read),
		supplierv1.SupplierService_GetSupplier_FullMethodName:      middleware.GarageMethod(read),
		supplierv1.SupplierService_CreateSupplier_FullMethodName:   middleware.GarageMethod(write),
		supplierv1.SupplierService_UpdateSupplier_FullMethodName:   middleware.GarageMethod(write),
		supplierv1.SupplierService_DeleteSupplier_FullMethodName:   middleware.GarageMethod(write),
		supplierv1.SupplierService_ActivateSupplier_FullMethodName: middleware.GarageMethod(write),
	}
}

// run calls fn with the caller's user id and converts use case errors
// to gRPC status errors.
func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.UserID(ctx))
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}

func badMask(path string) error {
	return apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
}

// stringField returns a pointer to the optional value, "" when unset
// (which clears the field).
func stringField(v *string) *string {
	if v == nil {
		empty := ""

		return &empty
	}

	return v
}
