package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

// ListStockLevels lists the garage's parts with their stock on hand
// (FR-INV-03).
func (s *Server) ListStockLevels(ctx context.Context, req *inventoryv1.ListStockLevelsRequest) (*inventoryv1.ListStockLevelsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListStockLevelsResponse, error) {
		page, err := s.uc.ListStockLevels(ctx, usecase.ListStockLevelsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			Query: req.GetQuery(), CategoryID: req.GetCategoryId(), Brand: req.GetBrand(),
			LowStock: req.GetLowStock(), IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListStockLevelsResponse{
			Data: make([]*inventoryv1.StockLevel, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, level := range page.Items {
			resp.Data = append(resp.Data, stockLevelToProto(ctx, level))
		}

		return resp, nil
	})
}

// GetStockLevel returns one part with its stock on hand.
func (s *Server) GetStockLevel(ctx context.Context, req *inventoryv1.GetStockLevelRequest) (*inventoryv1.GetStockLevelResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetStockLevelResponse, error) {
		level, err := s.uc.GetStockLevel(ctx, req.GetGarageId(), req.GetPartId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetStockLevelResponse{StockLevel: stockLevelToProto(ctx, level)}, nil
	})
}

// ListStockTransactions returns the stock in/out/on-hand movement report
// (FR-INV-05); the Excel export of the same data is the Document Service's.
func (s *Server) ListStockTransactions(ctx context.Context, req *inventoryv1.ListStockTransactionsRequest) (*inventoryv1.ListStockTransactionsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListStockTransactionsResponse, error) {
		page, err := s.uc.ListTransactions(ctx, usecase.ListTransactionsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			PartID: req.GetPartId(), Type: transactionTypeFromProto(req.GetType()),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListStockTransactionsResponse{
			Data: make([]*inventoryv1.StockTransaction, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, item := range page.Items {
			resp.Data = append(resp.Data, transactionToProto(ctx, item))
		}

		return resp, nil
	})
}
