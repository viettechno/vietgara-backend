// Package middleware provides shared gRPC interceptors used by every
// VietGara service.
//
// See: LLD Section 6 (RBAC + RLS Middleware), Security Architecture Section 3.
package middleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

type ctxKeyClaims struct{}
type ctxKeyGarageID struct{}

// GarageScoped is implemented by any gRPC request message that carries a
// garage_id field -- this matches the convention used by every generated
// .proto message that operates on a specific garage's data.
type GarageScoped interface {
	GetGarageId() string
}

// ClaimsFromContext retrieves the verified JWT claims set by AuthInterceptor.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(ctxKeyClaims{}).(*auth.Claims)
	return c, ok
}

// GarageIDFromContext retrieves the garage_id that TenantContextInterceptor
// has already validated for the current request.
func GarageIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyGarageID{}).(string)
	return id, ok
}

// AuthInterceptor verifies the bearer token from the "authorization" gRPC
// metadata and attaches the parsed claims to the context. Public endpoints
// (e.g. quote approval via a token link) must be registered separately and
// should NOT go through this interceptor -- see PublicTokenInterceptor.
func AuthInterceptor(issuer *auth.TokenIssuer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		tokens := md.Get("authorization")
		if len(tokens) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}
		claims, err := issuer.Verify(tokens[0])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = context.WithValue(ctx, ctxKeyClaims{}, claims)
		return handler(ctx, req)
	}
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
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		scoped, isScoped := req.(GarageScoped)
		if !isScoped {
			// Not every request operates on a specific garage (e.g. GET /plans).
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

// ChainUnaryInterceptors composes multiple interceptors into one, applied
// in the given order (first argument runs outermost).
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			next := chain
			chain = func(ctx context.Context, req any) (any, error) {
				return interceptor(ctx, req, info, next)
			}
		}
		return chain(ctx, req)
	}
}
