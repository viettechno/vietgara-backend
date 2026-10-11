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
// (verified) user's own. The
// garage in GetMyVehicleHistory's path is not a membership check -- the
// person is a customer there, not staff -- so the use case checks the
// vehicle is on one of their customer records instead.
func Policies() middleware.Policies {
	return middleware.Policies{
		customerportalv1.CustomerPortalService_ListMyVehicles_FullMethodName:      middleware.UserMethod(),
		customerportalv1.CustomerPortalService_GetMyVehicleHistory_FullMethodName: middleware.UserMethod(),
		customerportalv1.CustomerPortalService_ListMyGarageLinks_FullMethodName:   middleware.UserMethod(),
		customerportalv1.CustomerPortalService_AcceptGarageLink_FullMethodName:    middleware.UserMethod(),
		customerportalv1.CustomerPortalService_DeclineGarageLink_FullMethodName:   middleware.UserMethod(),
		customerportalv1.CustomerPortalService_UnlinkGarageLink_FullMethodName:    middleware.UserMethod(),
	}
}

// ListMyVehicles serves the Customer App's vehicle list (FR-CAPP-02).
func (s *Server) ListMyVehicles(
	ctx context.Context, _ *customerportalv1.ListMyVehiclesRequest,
) (*customerportalv1.ListMyVehiclesResponse, error) {
	garages, err := s.uc.ListMyVehicles(ctx, middleware.UserID(ctx))
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
	entries, err := s.uc.GetMyVehicleHistory(ctx, middleware.UserID(ctx), req.GetGarageId(), req.GetVehicleId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &customerportalv1.GetMyVehicleHistoryResponse{Data: make([]*customerv1.VehicleHistoryEntry, 0, len(entries))}
	for _, e := range entries {
		resp.Data = append(resp.Data, customerHandler.HistoryEntryToProto(e))
	}

	return resp, nil
}

// ListMyGarageLinks lists the link requests waiting for an answer and the
// garages already linked (FR-CAPP-05).
func (s *Server) ListMyGarageLinks(
	ctx context.Context, _ *customerportalv1.ListMyGarageLinksRequest,
) (*customerportalv1.ListMyGarageLinksResponse, error) {
	links, err := s.uc.ListMyGarageLinks(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &customerportalv1.ListMyGarageLinksResponse{Data: make([]*customerportalv1.GarageLink, 0, len(links))}
	for _, l := range links {
		resp.Data = append(resp.Data, garageLinkToProto(l))
	}

	return resp, nil
}

// AcceptGarageLink accepts a garage's link request.
func (s *Server) AcceptGarageLink(
	ctx context.Context, req *customerportalv1.AcceptGarageLinkRequest,
) (*customerportalv1.AcceptGarageLinkResponse, error) {
	link, err := s.uc.AcceptGarageLink(ctx, middleware.UserID(ctx), req.GetLinkId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &customerportalv1.AcceptGarageLinkResponse{Link: garageLinkToProto(link)}, nil
}

// DeclineGarageLink declines a garage's link request.
func (s *Server) DeclineGarageLink(
	ctx context.Context, req *customerportalv1.DeclineGarageLinkRequest,
) (*customerportalv1.DeclineGarageLinkResponse, error) {
	link, err := s.uc.DeclineGarageLink(ctx, middleware.UserID(ctx), req.GetLinkId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &customerportalv1.DeclineGarageLinkResponse{Link: garageLinkToProto(link)}, nil
}

// UnlinkGarageLink ends a link from the user's side.
func (s *Server) UnlinkGarageLink(
	ctx context.Context, req *customerportalv1.UnlinkGarageLinkRequest,
) (*customerportalv1.UnlinkGarageLinkResponse, error) {
	link, err := s.uc.UnlinkGarageLink(ctx, middleware.UserID(ctx), req.GetLinkId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &customerportalv1.UnlinkGarageLinkResponse{Link: garageLinkToProto(link)}, nil
}

func garageLinkToProto(l usecase.GarageLink) *customerportalv1.GarageLink {
	link := customerHandler.LinkToProto(l.Link)

	return &customerportalv1.GarageLink{
		Id: link.GetId(), GarageId: l.Link.GarageID, GarageName: l.Garage.Name, GarageAddress: l.Garage.Address,
		GaragePhone: l.Garage.Phone, CustomerId: l.Link.CustomerID, CustomerName: l.CustomerName,
		Status: link.GetStatus(), RequestedAt: link.GetRequestedAt(), LinkedAt: link.GetLinkedAt(),
	}
}
