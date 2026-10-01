package handler

import (
	"context"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
)

// SendQuote sends the quote for customer approval (FR-QUO-02).
func (s *Server) SendQuote(ctx context.Context, req *quotev1.SendQuoteRequest) (*quotev1.SendQuoteResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.SendQuoteResponse, error) {
		result, err := s.uc.Send(ctx, req.GetGarageId(), req.GetQuoteId(), actorID)
		if err != nil {
			return nil, err
		}

		return &quotev1.SendQuoteResponse{Quote: quoteToProto(result.Quote), PublicUrl: result.PublicURL, Emailed: result.Emailed}, nil
	})
}

// RecordQuoteDecision records the customer's decision on their behalf
// (FR-QUO-03): for a MANUAL-mode quote, or as a staff override of a
// LINK-mode one.
func (s *Server) RecordQuoteDecision(ctx context.Context, req *quotev1.RecordQuoteDecisionRequest) (*quotev1.RecordQuoteDecisionResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.RecordQuoteDecisionResponse, error) {
		q, err := s.uc.RecordQuoteDecision(ctx, req.GetGarageId(), req.GetQuoteId(), actorID, decisionsFromProto(req.GetDecisions()))
		if err != nil {
			return nil, err
		}

		return &quotev1.RecordQuoteDecisionResponse{Quote: quoteToProto(q)}, nil
	})
}

// ConvertQuote converts an approved quote into a draft repair order
// (FR-QUO-04).
func (s *Server) ConvertQuote(ctx context.Context, req *quotev1.ConvertQuoteRequest) (*quotev1.ConvertQuoteResponse, error) {
	return run(ctx, func(actorID string) (*quotev1.ConvertQuoteResponse, error) {
		result, err := s.uc.Convert(ctx, req.GetGarageId(), req.GetQuoteId(), actorID)
		if err != nil {
			return nil, err
		}

		return &quotev1.ConvertQuoteResponse{Quote: quoteToProto(result.Quote), RepairOrderId: result.RepairOrderID}, nil
	})
}
