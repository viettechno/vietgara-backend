// Package middleware provides the gRPC interceptors every VietGara request
// passes through -- the REST API reaches the same handlers through
// grpc-gateway, so these checks cover both protocols:
//
//	Auth -> EmailVerified -> PlatformAdmin -> Garage (tx + access + license)
//
// What each RPC requires is declared once in a Policy table assembled from
// every module (see each module's handler.Policies).
//
// See: LLD Section 6 (RBAC + RLS Middleware), Security Architecture Section 3.
package middleware

import (
	"context"
	"maps"
	"path"
	"slices"
	"strings"

	"google.golang.org/grpc"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// Policy is what one RPC requires from its caller.
type Policy struct {
	// Public RPCs need no access token (sign-in, password reset).
	Public bool
	// AllowUnverified lets users whose e-mail is not verified yet call
	// the RPC (profile, e-mail verification, plans).
	AllowUnverified bool
	// PlatformAdmin restricts the RPC to platform operators.
	PlatformAdmin bool
	// Garage is checked against the request's garage_id; nil for RPCs
	// that are not garage-scoped.
	Garage *access.Rule
	// SkipLicense exempts a garage-scoped RPC from subscription
	// enforcement (it must stay reachable to renew).
	SkipLicense bool
	// Idempotent RPCs require an Idempotency-Key and replay the first
	// response for a repeated key (see Idempotency). Garage-scoped only.
	Idempotent bool
}

// Policies maps full gRPC method names to their Policy.
type Policies map[string]Policy

// Merge returns the union of several policy tables.
func Merge(tables ...Policies) Policies {
	merged := Policies{}

	for _, t := range tables {
		maps.Copy(merged, t)
	}

	return merged
}

// PublicMethod needs no access token.
func PublicMethod() Policy { return Policy{Public: true} }

// UserMethod needs a verified user and no garage.
func UserMethod() Policy { return Policy{} }

// UnverifiedMethod needs any signed-in user.
func UnverifiedMethod() Policy { return Policy{AllowUnverified: true} }

// AdminMethod needs a platform admin.
func AdminMethod() Policy { return Policy{PlatformAdmin: true} }

// GarageMethod needs rule in the request's garage.
func GarageMethod(rule access.Rule) Policy { return Policy{Garage: &rule} }

// IdempotentGarageMethod is GarageMethod for a creation with financial
// impact, which must be sent with an Idempotency-Key.
func IdempotentGarageMethod(rule access.Rule) Policy {
	return Policy{Garage: &rule, Idempotent: true}
}

// ChainUnaryInterceptors composes multiple interceptors into one, applied
// in the given order (first argument runs outermost).
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chain := handler

		for _, interceptor := range slices.Backward(interceptors) {
			next := chain
			chain = func(ctx context.Context, req any) (any, error) {
				return interceptor(ctx, req, info, next)
			}
		}

		return chain(ctx, req)
	}
}

// isReadMethod reports whether an RPC only reads: Get*, List* and Lookup*.
func isReadMethod(fullMethod string) bool {
	name := path.Base(fullMethod)

	return strings.HasPrefix(name, "Get") || strings.HasPrefix(name, "List") || strings.HasPrefix(name, "Lookup")
}
