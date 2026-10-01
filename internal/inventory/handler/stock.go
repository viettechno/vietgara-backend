package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

// CreateStockReceipt records a goods-receipt note from a supplier
// (FR-INV-01).
func (s *Server) CreateStockReceipt(ctx context.Context, req *inventoryv1.CreateStockReceiptRequest) (*inventoryv1.CreateStockReceiptResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateStockReceiptResponse, error) {
		result, err := s.uc.CreateStockReceipt(ctx, usecase.CreateStockReceiptRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, ItemID: req.GetItemId(), Quantity: req.GetQuantity(),
			UnitCost: req.GetUnitCost(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CreateStockReceiptResponse{
			Transaction: transactionToProto(result.Transaction), Item: itemToProto(result.Item),
		}, nil
	})
}

// ListStockTransactions returns the stock in/out/on-hand movement report
// (FR-INV-05); Excel export of the same data is Release 1.4's
// Document/Print Service.
func (s *Server) ListStockTransactions(ctx context.Context, req *inventoryv1.ListStockTransactionsRequest) (*inventoryv1.ListStockTransactionsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListStockTransactionsResponse, error) {
		page, err := s.uc.ListTransactions(ctx, usecase.ListTransactionsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			ItemID: req.GetItemId(), Type: transactionTypeFromProto(req.GetType()),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListStockTransactionsResponse{
			Data: make([]*inventoryv1.StockTransaction, 0, len(page.Items)), Pagination: transactionPaginationToProto(page),
		}
		for _, item := range page.Items {
			resp.Data = append(resp.Data, transactionToProto(item))
		}

		return resp, nil
	})
}
