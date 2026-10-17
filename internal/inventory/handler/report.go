package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

// GetPurchasesReport lists purchases by supplier and period (FR-INV-19).
func (s *Server) GetPurchasesReport(ctx context.Context, req *inventoryv1.GetPurchasesReportRequest) (*inventoryv1.GetPurchasesReportResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetPurchasesReportResponse, error) {
		report, err := s.uc.PurchasesReport(ctx, usecase.PurchasesReportRequest{
			GarageID: req.GetGarageId(), SupplierID: req.GetSupplierId(), From: req.GetFrom(), To: req.GetTo(),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.GetPurchasesReportResponse{
			Data: make([]*inventoryv1.PurchaseLine, 0, len(report.Lines)), TotalAmount: report.TotalAmount,
		}

		for _, l := range report.Lines {
			resp.Data = append(resp.Data, &inventoryv1.PurchaseLine{
				GoodsReceiptId: l.GoodsReceiptID, GoodsReceiptNumber: l.GoodsReceiptNumber, ReceiptDate: l.ReceiptDate,
				SupplierId: l.SupplierID, SupplierName: l.SupplierName, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName,
				Quantity: l.Quantity, BaseUnitAbbreviation: l.BaseUnitAbbreviation, UnitCost: l.UnitCost, Amount: l.Amount,
			})
		}

		return resp, nil
	})
}

// GetStockValuationReport values the garage's stock at a date (FR-INV-07).
func (s *Server) GetStockValuationReport(ctx context.Context, req *inventoryv1.GetStockValuationReportRequest) (*inventoryv1.GetStockValuationReportResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetStockValuationReportResponse, error) {
		report, err := s.uc.StockValuation(ctx, req.GetGarageId(), req.GetAt())
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.GetStockValuationReportResponse{
			Data:       make([]*inventoryv1.StockValuationLine, 0, len(report.Lines)),
			Categories: make([]*inventoryv1.StockValuationCategory, 0, len(report.Categories)),
			TotalValue: report.TotalValue, At: report.At,
		}

		for _, l := range report.Lines {
			resp.Data = append(resp.Data, &inventoryv1.StockValuationLine{
				PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, CategoryId: l.CategoryID, CategoryName: l.CategoryName,
				QuantityOnHand: l.QuantityOnHand, BaseUnitAbbreviation: l.BaseUnitAbbreviation, AverageCost: l.AverageCost, Value: l.Value,
			})
		}

		for _, c := range report.Categories {
			resp.Categories = append(resp.Categories, &inventoryv1.StockValuationCategory{
				CategoryId: c.CategoryID, CategoryName: c.CategoryName, Value: c.Value,
			})
		}

		return resp, nil
	})
}
