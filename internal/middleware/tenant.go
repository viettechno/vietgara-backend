// Package middleware provides shared gRPC interceptors used by every
// VietGara service.
//
// See: LLD Section 6 (RBAC + RLS Middleware), Security Architecture Section 3.
package middleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

type ctxKeyGarageID struct{}

// GarageScoped is implemented by any gRPC request message that carries a
// garage_id field -- this matches the convention used by every generated
// .proto message that operates on a specific garage's data.
type GarageScoped interface {
	GetGarageId() string
}

// GarageIDFromContext retrieves the garage_id that TenantContextInterceptor
// has already validated for the current request.
func GarageIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyGarageID{}).(string)

	return id, ok
}

// TenantContextInterceptor extracts garage_id from the request (if the
// request implements GarageScoped), checks that the caller's JWT grants
// access to that garage, and stores the validated garage_id in the
// context for downstream usecases/repositories to use when opening a
// transaction (see db.TxManager.WithTransaction).
//
// This does NOT set the Postgres session variable itself -- that only
// happens once a transaction is actually opened, to keep this interceptor
// free of any database dependency.
//
// See: LLD Section 6, Database Design Section 1.2.
func TenantContextInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		scoped, isScoped := req.(GarageScoped)
		if !isScoped {
			// Not every request operates on a specific garage (e.g. GET /accounts).
			return handler(ctx, req)
		}

		garageID := scoped.GetGarageId()
		if garageID == "" {
			return nil, status.Error(codes.InvalidArgument, "garage_id is required")
		}

		claims, ok := ClaimsFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing authentication context")
		}

		if _, hasAccess := claims.HasAccessToGarage(garageID); !hasAccess {
			return nil, status.Error(codes.PermissionDenied, "no access to this garage")
		}

		ctx = context.WithValue(ctx, ctxKeyGarageID{}, garageID)

		return handler(ctx, req)
	}
}

// TransactionInterceptor runs every request whose garage_id was already
// validated by TenantContextInterceptor inside a single Postgres
// transaction, setting app.current_garage_id so the Row-Level Security
// policies on garages/garage_staff apply (see db.TxManager.WithTransaction).
// Requests without a garage context (e.g. creating a garage) pass through
// untransacted -- those open their own transaction inside the usecase.
// It must run AFTER TenantContextInterceptor in the interceptor chain.
func TransactionInterceptor(txManager *platformdb.TxManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		garageID, ok := GarageIDFromContext(ctx)
		if !ok {
			return handler(ctx, req)
		}

		var resp any

		err := txManager.WithTransaction(ctx, &garageID, func(txCtx context.Context) error {
			var err error

			resp, err = handler(txCtx, req)

			return err
		})
		if err != nil {
			return nil, err
		}

		return resp, nil
	}
}
