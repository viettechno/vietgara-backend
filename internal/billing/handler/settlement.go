package handler

import (
	"context"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
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

// AllocateSettlement splits the settlement between the customer and an
// insurer (FR-BIL-02).
func (s *Server) AllocateSettlement(
	ctx context.Context, req *billingv1.AllocateSettlementRequest,
) (*billingv1.AllocateSettlementResponse, error) {
	return run(ctx, func(actorID string) (*billingv1.AllocateSettlementResponse, error) {
		settlement, err := s.uc.Allocate(ctx, usecase.AllocateSettlementRequest{
			GarageID: req.GetGarageId(), SettlementID: req.GetSettlementId(), ActorID: actorID,
			CustomerAmount: req.GetCustomerAmount(), InsurerAmount: req.GetInsurerAmount(),
			InsurerPartnerID: req.InsurerPartnerId,
		})
		if err != nil {
			return nil, err
		}

		return &billingv1.AllocateSettlementResponse{Settlement: settlementToProto(settlement)}, nil
	})
}

// RecordSettlementPayment records a payment actually collected (FR-BIL-03).
func (s *Server) RecordSettlementPayment(
	ctx context.Context, req *billingv1.RecordSettlementPaymentRequest,
) (*billingv1.RecordSettlementPaymentResponse, error) {
	return run(ctx, func(actorID string) (*billingv1.RecordSettlementPaymentResponse, error) {
		settlement, err := s.uc.RecordPayment(ctx, usecase.RecordPaymentRequest{
			GarageID: req.GetGarageId(), SettlementID: req.GetSettlementId(), ActorID: actorID,
			PayerType: payerTypeFromProto(req.GetPayerType()), Amount: req.GetAmount(),
			Method: methodFromProto(req.GetMethod()), Note: req.GetNote(),
		})
		if err != nil {
			return nil, err
		}

		return &billingv1.RecordSettlementPaymentResponse{Settlement: settlementToProto(settlement)}, nil
	})
}
