// Package handler exposes the Billing & Settlement module (FRD 3.8/3.9)
// as a gRPC service, served as REST through grpc-gateway. Authorization
// happens in the request middleware from the policies below.
package handler

import (
	"context"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements billingv1.SettlementServiceServer.
type Server struct {
	billingv1.UnimplementedSettlementServiceServer

	uc *usecase.SettlementUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.SettlementUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.BillingRead), access.Requires(access.BillingWrite)

	return middleware.Policies{
		billingv1.SettlementService_ListSettlements_FullMethodName:         middleware.GarageMethod(read),
		billingv1.SettlementService_CreateSettlement_FullMethodName:        middleware.IdempotentGarageMethod(write),
		billingv1.SettlementService_GetSettlement_FullMethodName:           middleware.GarageMethod(read),
		billingv1.SettlementService_AllocateSettlement_FullMethodName:      middleware.GarageMethod(write),
		billingv1.SettlementService_RecordSettlementPayment_FullMethodName: middleware.IdempotentGarageMethod(write),
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
