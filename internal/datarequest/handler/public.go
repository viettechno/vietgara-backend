package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// PublicServer implements customerv1.PublicDataRequestServiceServer: the
// request page for people without the app.
type PublicServer struct {
	customerv1.UnimplementedPublicDataRequestServiceServer

	uc *usecase.PublicUseCase
}

// NewPublicServer wires the handler to its use case.
func NewPublicServer(uc *usecase.PublicUseCase) *PublicServer { return &PublicServer{uc: uc} }

// PublicPolicies declares both calls public: no access token, and the use case
// throttles them.
func PublicPolicies() middleware.Policies {
	return middleware.Policies{
		customerv1.PublicDataRequestService_SubmitPublicDataRequest_FullMethodName:  middleware.PublicMethod(),
		customerv1.PublicDataRequestService_ConfirmPublicDataRequest_FullMethodName: middleware.PublicMethod(),
	}
}

// SubmitPublicDataRequest records a request and e-mails its confirmation link.
// It answers the same whether or not the address is known.
func (s *PublicServer) SubmitPublicDataRequest(ctx context.Context, req *customerv1.SubmitPublicDataRequestRequest) (*customerv1.SubmitPublicDataRequestResponse, error) {
	err := s.uc.Submit(ctx, usecase.SubmitRequest{
		Email: req.GetEmail(), Type: typeFromProto(req.GetType()), Details: req.GetDetails(), Locale: req.GetLocale(),
		SourceIP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &customerv1.SubmitPublicDataRequestResponse{}, nil
}

// ConfirmPublicDataRequest acts on a confirmed request.
func (s *PublicServer) ConfirmPublicDataRequest(ctx context.Context, req *customerv1.ConfirmPublicDataRequestRequest) (*customerv1.ConfirmPublicDataRequestResponse, error) {
	n, err := s.uc.Confirm(ctx, req.GetToken())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &customerv1.ConfirmPublicDataRequestResponse{GarageCount: toInt32(n)}, nil
}
