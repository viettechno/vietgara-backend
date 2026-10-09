// Package handler exposes the Customer & Vehicle module (FRD 3.4) as gRPC
// services -- customers, vehicles and partners -- served as REST through
// grpc-gateway. Authorization happens in the request middleware from the
// policies below.
package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements customerv1.CustomerServiceServer,
// customerv1.VehicleServiceServer and customerv1.PartnerServiceServer.
type Server struct {
	customerv1.UnimplementedCustomerServiceServer
	customerv1.UnimplementedVehicleServiceServer
	customerv1.UnimplementedPartnerServiceServer

	customers *usecase.CustomerUseCase
	vehicles  *usecase.VehicleUseCase
	partners  *usecase.PartnerUseCase
}

// NewServer wires the handler to its use cases.
func NewServer(customers *usecase.CustomerUseCase, vehicles *usecase.VehicleUseCase, partners *usecase.PartnerUseCase) *Server {
	return &Server{customers: customers, vehicles: vehicles, partners: partners}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.CustomerRead), access.Requires(access.CustomerWrite)
	partnerRead, partnerWrite := access.Requires(access.PartnerRead), access.Requires(access.PartnerWrite)

	return middleware.Policies{
		customerv1.CustomerService_ListCustomers_FullMethodName:  middleware.GarageMethod(read),
		customerv1.CustomerService_GetCustomer_FullMethodName:    middleware.GarageMethod(read),
		customerv1.CustomerService_CreateCustomer_FullMethodName: middleware.GarageMethod(write),
		customerv1.CustomerService_UpdateCustomer_FullMethodName: middleware.GarageMethod(write),
		customerv1.CustomerService_DeleteCustomer_FullMethodName: middleware.GarageMethod(write),

		customerv1.VehicleService_ListVehicles_FullMethodName:      middleware.GarageMethod(read),
		customerv1.VehicleService_GetVehicle_FullMethodName:        middleware.GarageMethod(read),
		customerv1.VehicleService_CreateVehicle_FullMethodName:     middleware.GarageMethod(write),
		customerv1.VehicleService_UpdateVehicle_FullMethodName:     middleware.GarageMethod(write),
		customerv1.VehicleService_DeleteVehicle_FullMethodName:     middleware.GarageMethod(write),
		customerv1.VehicleService_GetVehicleHistory_FullMethodName: middleware.GarageMethod(read),

		customerv1.PartnerService_ListPartners_FullMethodName:  middleware.GarageMethod(partnerRead),
		customerv1.PartnerService_GetPartner_FullMethodName:    middleware.GarageMethod(partnerRead),
		customerv1.PartnerService_CreatePartner_FullMethodName: middleware.GarageMethod(partnerWrite),
		customerv1.PartnerService_UpdatePartner_FullMethodName: middleware.GarageMethod(partnerWrite),
		customerv1.PartnerService_DeletePartner_FullMethodName: middleware.GarageMethod(partnerWrite),
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

// maskPaths returns the update mask paths, or settable when it is empty.
func maskPaths(paths []string, settable ...string) []string {
	if len(paths) == 0 {
		return settable
	}

	return paths
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
