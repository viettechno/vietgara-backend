package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Reasons reported by the interceptors of this package.
const (
	ReasonEmailNotVerified = "EMAIL_NOT_VERIFIED"
	ReasonNoPolicy         = "NO_POLICY"
)

type ctxKeyClaims struct{}

type ctxKeyPolicy struct{}

// WithClaims stores verified token claims in ctx (tests and background
// jobs acting for a user).
func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(ctx, ctxKeyClaims{}, claims)
}

// ClaimsFromContext retrieves the verified JWT claims set by Auth.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(ctxKeyClaims{}).(*auth.Claims)

	return c, ok
}

// UserID returns the signed-in user, or "".
func UserID(ctx context.Context) string {
	if c, ok := ClaimsFromContext(ctx); ok {
		return c.UserID()
	}

	return ""
}

func policyFromContext(ctx context.Context) Policy {
	p, _ := ctx.Value(ctxKeyPolicy{}).(Policy)

	return p
}

// Auth looks up the RPC's policy and, unless it is public, verifies the
// bearer token from the "authorization" metadata and stores its claims.
// RPCs missing from policies are rejected (fail closed).
func Auth(issuer *auth.TokenIssuer, policies Policies) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		policy, known := policies[info.FullMethod]
		if !known {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodePermissionDenied, "this method is not available").WithReason(ReasonNoPolicy))
		}

		ctx = context.WithValue(ctx, ctxKeyPolicy{}, policy)
		if policy.Public {
			return handler(ctx, req)
		}

		token := firstMetadata(ctx, "authorization")
		if token == "" {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodeUnauthenticated, "missing authorization token"))
		}

		claims, err := issuer.Verify(token)
		if err != nil {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodeUnauthenticated, "invalid or expired token"))
		}

		return handler(WithClaims(ctx, claims), req)
	}
}

// EmailVerified rejects users that have not verified their e-mail yet,
// except on RPCs that allow it (FR-IAM-01).
func EmailVerified() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		policy := policyFromContext(ctx)
		if policy.Public || policy.AllowUnverified {
			return handler(ctx, req)
		}

		if claims, ok := ClaimsFromContext(ctx); !ok || !claims.EmailVerified {
			return nil, apperror.ToGRPCError(apperror.New(apperror.CodePermissionDenied, "verify your e-mail address first").WithReason(ReasonEmailNotVerified))
		}

		return handler(ctx, req)
	}
}

// ClientIP returns the caller's address for the audit log. The API gateway
// sets X-Real-IP to the client address it resolved (it overwrites any value
// the client sent); X-Forwarded-For is only the fallback for a backend
// reached without the gateway, and its first entry is whatever the outermost
// proxy trusts, so it is not an authority.
func ClientIP(ctx context.Context) string {
	if realIP := strings.TrimSpace(firstMetadata(ctx, "x-real-ip")); realIP != "" {
		return realIP
	}

	forwarded := firstMetadata(ctx, "x-forwarded-for")
	if first, _, found := strings.Cut(forwarded, ","); found {
		return strings.TrimSpace(first)
	}

	return strings.TrimSpace(forwarded)
}

func firstMetadata(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}

	return ""
}
