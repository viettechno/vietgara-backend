// Package handler exposes the Technician BFF (Release 2.2) as a gRPC
// service, served as REST through grpc-gateway.
package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	technicianv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/technician/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	roHandler "github.com/viettechno/vietgara-backend/internal/repairorder/handler"
	"github.com/viettechno/vietgara-backend/internal/technician/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements technicianv1.TechnicianServiceServer.
type Server struct {
	technicianv1.UnimplementedTechnicianServiceServer

	uc *usecase.TechnicianUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.TechnicianUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the RPC's requirements: the work list is the
// signed-in (verified) user's own,
// across garages; each garage's access is checked by the use case.
func Policies() middleware.Policies {
	return middleware.Policies{
		technicianv1.TechnicianService_ListMyWork_FullMethodName: middleware.UserMethod(),
	}
}

// ListMyWork serves the Technician App's work list (FR-TAPP-01, UI Design
// Spec 5.1).
func (s *Server) ListMyWork(ctx context.Context, _ *technicianv1.ListMyWorkRequest) (*technicianv1.ListMyWorkResponse, error) {
	work, err := s.uc.ListMyWork(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &technicianv1.ListMyWorkResponse{Data: make([]*technicianv1.WorkOrder, 0, len(work))}

	for _, w := range work {
		ro := roHandler.RepairOrderToProto(w.RepairOrder)
		resp.Data = append(resp.Data, &technicianv1.WorkOrder{
			GarageId:            w.GarageID,
			GarageName:          w.GarageName,
			RepairOrderId:       ro.GetId(),
			Number:              ro.GetNumber(),
			Status:              ro.GetStatus(),
			VehicleLicensePlate: ro.GetVehicleLicensePlate(),
			CustomerName:        ro.GetCustomerName(),
			CreatedAt:           timestamppb.New(w.RepairOrder.CreatedAt),
			Lines:               ro.GetLines(),
		})
	}

	return resp, nil
}
