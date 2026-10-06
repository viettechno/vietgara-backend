// Package handler exposes the Customer BFF (Release 2.4) as a gRPC service,
// served as REST through grpc-gateway.
package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	customerportalv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customerportal/v1"

	customerHandler "github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/customerportal/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements customerportalv1.CustomerPortalServiceServer.
type Server struct {
	customerportalv1.UnimplementedCustomerPortalServiceServer

	uc *usecase.PortalUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.PortalUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the RPCs' requirements: both are the signed-in
// (verified) account's own. The
// garage in GetMyVehicleHistory's path is not a membership check -- the
// person is a customer there, not staff -- so the use case checks the
// vehicle is on one of their customer records instead.
func Policies() middleware.Policies {
	return middleware.Policies{
		customerportalv1.CustomerPortalService_ListMyVehicles_FullMethodName:      middleware.AccountMethod(),
		customerportalv1.CustomerPortalService_GetMyVehicleHistory_FullMethodName: middleware.AccountMethod(),
	}
}

// ListMyVehicles serves the Customer App's vehicle list (FR-CAPP-02).
func (s *Server) ListMyVehicles(
	ctx context.Context, _ *customerportalv1.ListMyVehiclesRequest,
) (*customerportalv1.ListMyVehiclesResponse, error) {
	garages, err := s.uc.ListMyVehicles(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &customerportalv1.ListMyVehiclesResponse{Data: make([]*customerportalv1.MyGarage, 0, len(garages))}

	for _, g := range garages {
		vehicles := make([]*customerv1.Vehicle, 0, len(g.Vehicles))
		for _, v := range g.Vehicles {
			vehicles = append(vehicles, customerHandler.VehicleToProto(v))
		}

		resp.Data = append(resp.Data, &customerportalv1.MyGarage{
			GarageId: g.GarageID, Name: g.Garage.Name, Address: g.Garage.Address, Phone: g.Garage.Phone,
			CustomerId: g.CustomerID, CustomerName: g.CustomerName, Vehicles: vehicles,
		})
	}

	return resp, nil
}

// GetMyVehicleHistory serves a vehicle's repair history (FR-CAPP-02).
func (s *Server) GetMyVehicleHistory(
	ctx context.Context, req *customerportalv1.GetMyVehicleHistoryRequest,
) (*customerportalv1.GetMyVehicleHistoryResponse, error) {
	entries, err := s.uc.GetMyVehicleHistory(ctx, middleware.AccountID(ctx), req.GetGarageId(), req.GetVehicleId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &customerportalv1.GetMyVehicleHistoryResponse{Data: make([]*customerv1.VehicleHistoryEntry, 0, len(entries))}
	for _, e := range entries {
		resp.Data = append(resp.Data, customerHandler.HistoryEntryToProto(e))
	}

	return resp, nil
}
