// Package handler exposes the Inventory module (FRD 3.7) as a gRPC
// service, served as REST through grpc-gateway. Authorization happens in
// the request middleware from the policies below.
package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements inventoryv1.InventoryServiceServer.
type Server struct {
	inventoryv1.UnimplementedInventoryServiceServer

	uc *usecase.InventoryUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.InventoryUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.InventoryRead), access.Requires(access.InventoryWrite)

	return middleware.Policies{
		inventoryv1.InventoryService_ListStockLevels_FullMethodName:       middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetStockLevel_FullMethodName:         middleware.GarageMethod(read),
		inventoryv1.InventoryService_CreateStockReceipt_FullMethodName:    middleware.GarageMethod(write),
		inventoryv1.InventoryService_ListStockTransactions_FullMethodName: middleware.GarageMethod(read),
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
