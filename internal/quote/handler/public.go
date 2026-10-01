package handler

import (
	"context"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
)

// GetPublicQuote returns the quote an approval link's token names
// (FR-QUO-02), for the customer to review before deciding. No access
// token: the token in the path is the authorization.
func (s *Server) GetPublicQuote(ctx context.Context, req *quotev1.GetPublicQuoteRequest) (*quotev1.GetPublicQuoteResponse, error) {
	return public(func() (*quotev1.GetPublicQuoteResponse, error) {
		q, err := s.uc.GetPublicQuote(ctx, req.GetToken())
		if err != nil {
			return nil, err
		}

		return &quotev1.GetPublicQuoteResponse{Quote: publicQuoteToProto(q)}, nil
	})
}

// DecidePublicQuote records the customer's own decision (FR-QUO-03) with
// no login.
func (s *Server) DecidePublicQuote(ctx context.Context, req *quotev1.DecidePublicQuoteRequest) (*quotev1.DecidePublicQuoteResponse, error) {
	return public(func() (*quotev1.DecidePublicQuoteResponse, error) {
		q, err := s.uc.DecidePublicQuote(ctx, req.GetToken(), decisionsFromProto(req.GetDecisions()))
		if err != nil {
			return nil, err
		}

		return &quotev1.DecidePublicQuoteResponse{Quote: publicQuoteToProto(q)}, nil
	})
}
