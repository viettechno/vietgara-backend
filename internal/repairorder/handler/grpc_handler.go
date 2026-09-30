// Package handler exposes the Repair Order module (FRD 3.6) as a gRPC
// service, served as REST through grpc-gateway. Authorization happens in
// the request middleware from the policies below.
package handler

import (
	"context"

	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements repairorderv1.RepairOrderServiceServer.
type Server struct {
	repairorderv1.UnimplementedRepairOrderServiceServer

	uc *usecase.RepairOrderUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.RepairOrderUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.RepairOrderRead), access.Requires(access.RepairOrderWrite)

	return middleware.Policies{
		repairorderv1.RepairOrderService_ListRepairOrders_FullMethodName:      middleware.GarageMethod(read),
		repairorderv1.RepairOrderService_CreateRepairOrder_FullMethodName:     middleware.GarageMethod(write),
		repairorderv1.RepairOrderService_GetRepairOrder_FullMethodName:        middleware.GarageMethod(read),
		repairorderv1.RepairOrderService_UpdateRepairOrderLine_FullMethodName: middleware.GarageMethod(write),
		repairorderv1.RepairOrderService_CloseRepairOrder_FullMethodName:      middleware.GarageMethod(write),
	}
}

// run calls fn with the caller's account id and converts use case errors
// to gRPC status errors.
func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.AccountID(ctx))
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
