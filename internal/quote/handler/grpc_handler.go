// Package handler exposes the Quote & Pricing module (FRD 3.5) as gRPC
// services -- QuoteService (garage-scoped) and PublicQuoteService (the
// no-login customer approval link, FR-QUO-02) -- served as REST through
// grpc-gateway. Authorization happens in the request middleware from the
// policies below.
package handler

import (
	"context"

	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements quotev1.QuoteServiceServer and
// quotev1.PublicQuoteServiceServer.
type Server struct {
	quotev1.UnimplementedQuoteServiceServer
	quotev1.UnimplementedPublicQuoteServiceServer

	uc *usecase.QuoteUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.QuoteUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage; the
// public approval link needs none (the token in the path is the
// authorization, Security Architecture Section 2).
func Policies() middleware.Policies {
	read, write := access.Requires(access.QuoteRead), access.Requires(access.QuoteWrite)

	return middleware.Policies{
		quotev1.QuoteService_ListQuotes_FullMethodName:          middleware.GarageMethod(read),
		quotev1.QuoteService_GetQuote_FullMethodName:            middleware.GarageMethod(read),
		quotev1.QuoteService_CreateQuote_FullMethodName:         middleware.GarageMethod(write),
		quotev1.QuoteService_UpdateQuote_FullMethodName:         middleware.GarageMethod(write),
		quotev1.QuoteService_DeleteQuote_FullMethodName:         middleware.GarageMethod(write),
		quotev1.QuoteService_SendQuote_FullMethodName:           middleware.GarageMethod(write),
		quotev1.QuoteService_RecordQuoteDecision_FullMethodName: middleware.GarageMethod(write),
		quotev1.QuoteService_ConvertQuote_FullMethodName:        middleware.GarageMethod(write),

		quotev1.PublicQuoteService_GetPublicQuote_FullMethodName:    middleware.PublicMethod(),
		quotev1.PublicQuoteService_DecidePublicQuote_FullMethodName: middleware.PublicMethod(),
	}
}

// run calls fn with the caller's account id and converts use case errors
// to gRPC status errors.
func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.AccountID(ctx))
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}

// public calls fn (no account id: the caller is not signed in) and
// converts use case errors to gRPC status errors.
func public[T any](fn func() (T, error)) (T, error) {
	resp, err := fn()
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}
