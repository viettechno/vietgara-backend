package middleware

import (
	"context"
	"slices"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ctxKeyClaims struct{}

// ClaimsFromContext retrieves the verified JWT claims set by AuthInterceptor.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(ctxKeyClaims{}).(*auth.Claims)

	return c, ok
}

type authInterceptorConfig struct {
	// skipMethods lists full gRPC method names (e.g.
	// "/vietgara.identity.v1.AuthService/Register") that are public and
	// must NOT be rejected for missing credentials.
	skipMethods []string
}

// AuthInterceptorOption customizes AuthInterceptor.
type AuthInterceptorOption func(*authInterceptorConfig)

// WithSkipMethods marks the given full method names as public, letting
// them through without a bearer token. Used for unauthenticated endpoints
// such as Register, Login, RefreshToken and Logout.
func WithSkipMethods(methods ...string) AuthInterceptorOption {
	return func(c *authInterceptorConfig) {
		c.skipMethods = append(c.skipMethods, methods...)
	}
}

// AuthInterceptor verifies the bearer token from the "authorization" gRPC
// metadata and attaches the parsed claims to the context. Methods listed
// via WithSkipMethods bypass authentication -- everything else requires a
// valid token.
func AuthInterceptor(issuer *auth.TokenIssuer, options ...AuthInterceptorOption) grpc.UnaryServerInterceptor {
	config := &authInterceptorConfig{}
	for _, opt := range options {
		opt(config)
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info != nil && slices.Contains(config.skipMethods, info.FullMethod) {
			return handler(ctx, req)
		}

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
