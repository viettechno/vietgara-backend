package handler

import (
	"context"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// ListSettlements lists a garage's settlements, optionally filtered by
// status; also FR-REC-01's receivables view via outstanding_only.
func (s *Server) ListSettlements(
	ctx context.Context, req *billingv1.ListSettlementsRequest,
) (*billingv1.ListSettlementsResponse, error) {
	return run(ctx, func(string) (*billingv1.ListSettlementsResponse, error) {
		page, err := s.uc.List(ctx, usecase.ListSettlementsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			Status: statusFromProto(req.GetStatus()), OutstandingOnly: req.GetOutstandingOnly(),
		})
		if err != nil {
			return nil, err
		}

		resp := &billingv1.ListSettlementsResponse{
			Data: make([]*billingv1.Settlement, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, settlement := range page.Items {
			resp.Data = append(resp.Data, settlementToProto(settlement))
		}

		return resp, nil
	})
}

// CreateSettlement creates a settlement from a closed repair order
// (FR-BIL-01).
func (s *Server) CreateSettlement(
	ctx context.Context, req *billingv1.CreateSettlementRequest,
) (*billingv1.CreateSettlementResponse, error) {
	return run(ctx, func(actorID string) (*billingv1.CreateSettlementResponse, error) {
		settlement, err := s.uc.Create(ctx, usecase.CreateSettlementRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, RepairOrderID: req.GetRepairOrderId(),
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &billingv1.CreateSettlementResponse{Settlement: settlementToProto(settlement)}, nil
	})
}

// GetSettlement returns one settlement with its payments.
func (s *Server) GetSettlement(
	ctx context.Context, req *billingv1.GetSettlementRequest,
) (*billingv1.GetSettlementResponse, error) {
	return run(ctx, func(string) (*billingv1.GetSettlementResponse, error) {
		settlement, err := s.uc.Get(ctx, req.GetGarageId(), req.GetSettlementId())
		if err != nil {
			return nil, err
		}

		return &billingv1.GetSettlementResponse{Settlement: settlementToProto(settlement)}, nil
	})
}

// RecordSettlementPayment records a payment actually collected (FR-BIL-03).
func (s *Server) RecordSettlementPayment(
	ctx context.Context, req *billingv1.RecordSettlementPaymentRequest,
) (*billingv1.RecordSettlementPaymentResponse, error) {
	return run(ctx, func(actorID string) (*billingv1.RecordSettlementPaymentResponse, error) {
		settlement, err := s.uc.RecordPayment(ctx, usecase.RecordPaymentRequest{
			GarageID: req.GetGarageId(), SettlementID: req.GetSettlementId(), ActorID: actorID,
			Amount: req.GetAmount(),
			Method: methodFromProto(req.GetMethod()), Note: req.GetNote(),
			PayerName: req.GetPayerName(), PayerAddress: req.GetPayerAddress(), Reason: req.GetReason(),
		})
		if err != nil {
			return nil, err
		}

		return &billingv1.RecordSettlementPaymentResponse{Settlement: settlementToProto(settlement)}, nil
	})
}
