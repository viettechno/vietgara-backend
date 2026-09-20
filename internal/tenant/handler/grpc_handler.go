// Package handler wires the Tenant/Garage usecases to the gRPC services
// generated from vietgara-proto (proto/vietgara/tenant/v1).
package handler

import (
	"context"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements tenantv1.GarageServiceServer and
// tenantv1.StaffServiceServer.
//
// UpdateStaff and RemoveStaff are not implemented yet: the StaffUseCase /
// repository layers have no update/remove operations, so calls fall through
// to the embedded Unimplemented servers and return codes.Unimplemented.
type Server struct {
	tenantv1.UnimplementedGarageServiceServer
	tenantv1.UnimplementedStaffServiceServer

	garage *usecase.GarageUseCase
	staff  *usecase.StaffUseCase
}

// NewServer wires the handler to its use cases and returns a ready Server.
func NewServer(
	garage *usecase.GarageUseCase,
	staff *usecase.StaffUseCase,
) *Server {
	return &Server{
		garage: garage,
		staff:  staff,
	}
}

// accountIDFromContext reads the account_id (JWT "sub" claim) attached by
// middleware.AuthInterceptor.
func accountIDFromContext(ctx context.Context) (string, error) {
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing account context")
	}

	return claims.Subject, nil
}
