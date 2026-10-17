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
	cost := access.Requires(access.InventoryCostRead)
	adjust, count := access.Requires(access.InventoryAdjust), access.Requires(access.InventoryCount)

	return middleware.Policies{
		inventoryv1.InventoryService_ListStockLevels_FullMethodName:            middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetStockLevel_FullMethodName:              middleware.GarageMethod(read),
		inventoryv1.InventoryService_ListStockTransactions_FullMethodName:      middleware.GarageMethod(read),
		inventoryv1.InventoryService_ListGoodsReceipts_FullMethodName:          middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetGoodsReceipt_FullMethodName:            middleware.GarageMethod(read),
		inventoryv1.InventoryService_CreateGoodsReceipt_FullMethodName:         middleware.GarageMethod(write),
		inventoryv1.InventoryService_UpdateGoodsReceipt_FullMethodName:         middleware.GarageMethod(write),
		inventoryv1.InventoryService_CompleteGoodsReceipt_FullMethodName:       middleware.GarageMethod(write),
		inventoryv1.InventoryService_CancelGoodsReceipt_FullMethodName:         middleware.GarageMethod(write),
		inventoryv1.InventoryService_GetPartSuppliers_FullMethodName:           middleware.GarageMethod(read),
		inventoryv1.InventoryService_PutPartSuppliers_FullMethodName:           middleware.GarageMethod(write),
		inventoryv1.InventoryService_GetRepairOrderStockIssue_FullMethodName:   middleware.GarageMethod(read),
		inventoryv1.InventoryService_PostStockIssue_FullMethodName:             middleware.GarageMethod(write),
		inventoryv1.InventoryService_ListStockCounts_FullMethodName:            middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetStockCount_FullMethodName:              middleware.GarageMethod(read),
		inventoryv1.InventoryService_CreateStockCount_FullMethodName:           middleware.GarageMethod(count),
		inventoryv1.InventoryService_UpdateStockCount_FullMethodName:           middleware.GarageMethod(count),
		inventoryv1.InventoryService_AddStockCountLines_FullMethodName:         middleware.GarageMethod(count),
		inventoryv1.InventoryService_UpdateStockCountLine_FullMethodName:       middleware.GarageMethod(count),
		inventoryv1.InventoryService_DeleteStockCountLine_FullMethodName:       middleware.GarageMethod(count),
		inventoryv1.InventoryService_MergeStockCounts_FullMethodName:           middleware.GarageMethod(count),
		inventoryv1.InventoryService_CompleteStockCount_FullMethodName:         middleware.GarageMethod(count),
		inventoryv1.InventoryService_CancelStockCount_FullMethodName:           middleware.GarageMethod(count),
		inventoryv1.InventoryService_ListWriteOffs_FullMethodName:              middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetWriteOff_FullMethodName:                middleware.GarageMethod(read),
		inventoryv1.InventoryService_CreateWriteOff_FullMethodName:             middleware.GarageMethod(adjust),
		inventoryv1.InventoryService_CancelWriteOff_FullMethodName:             middleware.GarageMethod(adjust),
		inventoryv1.InventoryService_ListSupplierReturns_FullMethodName:        middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetSupplierReturn_FullMethodName:          middleware.GarageMethod(read),
		inventoryv1.InventoryService_CreateSupplierReturn_FullMethodName:       middleware.GarageMethod(adjust),
		inventoryv1.InventoryService_CancelSupplierReturn_FullMethodName:       middleware.GarageMethod(adjust),
		inventoryv1.InventoryService_ReturnRepairOrderPart_FullMethodName:      middleware.GarageMethod(adjust),
		inventoryv1.InventoryService_ListRepairOrderPartReturns_FullMethodName: middleware.GarageMethod(read),
		inventoryv1.InventoryService_GetPurchasesReport_FullMethodName:         middleware.GarageMethod(cost),
		inventoryv1.InventoryService_GetStockValuationReport_FullMethodName:    middleware.GarageMethod(cost),
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
