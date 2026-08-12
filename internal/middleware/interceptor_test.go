package middleware_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const testRoute = "/vietgara.tenant.v1.GarageService/GetGarage"

func newTestIssuer(t *testing.T) *auth.TokenIssuer {
	t.Helper()
	return auth.NewTokenIssuer([]byte("test-signing-key"), 15*time.Minute, 24*time.Hour)
}

func issueToken(t *testing.T, issuer *auth.TokenIssuer, accountID string, garages []auth.GarageClaim) string {
	t.Helper()

	token, _, err := issuer.IssueAccessToken(accountID, garages)
	require.NoError(t, err)

	return token
}

// authedCtx returns the context produced by AuthInterceptor for the given
// issuer's token, mirroring how service entrypoints seed authenticated
// contexts from gRPC metadata.
func authedCtx(t *testing.T, issuer *auth.TokenIssuer, token string) context.Context {
	t.Helper()
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", token))

	gotCtx := make(chan context.Context, 1)
	interceptor := middleware.AuthInterceptor(issuer)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		gotCtx <- ctx
		return nil, nil
	})
	require.NoError(t, err)

	return <-gotCtx
}

// testHandler returns a handler that delivers the context it receives to
// captured, so tests can inspect the context built by the interceptor.
func testHandler(captured chan<- context.Context) grpc.UnaryHandler {
	return func(ctx context.Context, req any) (any, error) {
		captured <- ctx
		return "handled", nil
	}
}

type garageScopedReq struct {
	garageID string
}

func (r garageScopedReq) GetGarageId() string {
	return r.garageID
}

func TestAuthInterceptor(t *testing.T) {
	issuer := newTestIssuer(t)
	validToken := issueToken(t, issuer, "acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})

	tests := []struct {
		name     string
		ctx      context.Context
		wantCode codes.Code
	}{
		{name: "missing metadata", ctx: t.Context(), wantCode: codes.Unauthenticated},
		{name: "missing authorization header", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-other", "v")), wantCode: codes.Unauthenticated},
		{name: "invalid token", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "not-a-jwt")), wantCode: codes.Unauthenticated},
		{name: "valid token", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", validToken)), wantCode: codes.OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := middleware.AuthInterceptor(issuer)
			handlerCtx := make(chan context.Context, 1)

			got, err := interceptor(tt.ctx, nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, testHandler(handlerCtx))

			if tt.wantCode != codes.OK {
				assert.Equal(t, tt.wantCode, status.Code(err))
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "handled", got)

			claims, ok := middleware.ClaimsFromContext(<-handlerCtx)
			require.True(t, ok)
			assert.Equal(t, "acc-1", claims.Subject)
		})
	}
}

func TestTenantContextInterceptor(t *testing.T) {
	issuer := newTestIssuer(t)
	accessGarage1 := authedCtx(t, issuer, issueToken(t, issuer, "acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}}))
	accessGarage2 := authedCtx(t, issuer, issueToken(t, issuer, "acc-1", []auth.GarageClaim{{GarageID: "garage-2", Role: "staff"}}))

	tests := []struct {
		name          string
		ctx           context.Context
		req           any
		wantCode      codes.Code
		wantGarageID  string
		wantHasGarage bool
	}{
		{name: "request not garage scoped passes through", ctx: t.Context(), req: struct{}{}, wantCode: codes.OK},
		{name: "missing garage id", ctx: t.Context(), req: garageScopedReq{garageID: ""}, wantCode: codes.InvalidArgument},
		{name: "missing authentication context", ctx: t.Context(), req: garageScopedReq{garageID: "garage-1"}, wantCode: codes.Unauthenticated},
		{name: "no access to garage", ctx: accessGarage2, req: garageScopedReq{garageID: "garage-1"}, wantCode: codes.PermissionDenied},
		{name: "access granted stores garage id", ctx: accessGarage1, req: garageScopedReq{garageID: "garage-1"}, wantCode: codes.OK, wantGarageID: "garage-1", wantHasGarage: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := middleware.TenantContextInterceptor()
			handlerCtx := make(chan context.Context, 1)

			got, err := interceptor(tt.ctx, tt.req, &grpc.UnaryServerInfo{FullMethod: testRoute}, testHandler(handlerCtx))

			if tt.wantCode != codes.OK {
				assert.Equal(t, tt.wantCode, status.Code(err))
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "handled", got)

			garageID, ok := middleware.GarageIDFromContext(<-handlerCtx)
			assert.Equal(t, tt.wantHasGarage, ok)
			assert.Equal(t, tt.wantGarageID, garageID)
		})
	}
}

func TestChainUnaryInterceptors(t *testing.T) {
	tests := []struct {
		name      string
		failFirst bool
		wantOrder []string
		wantErr   bool
	}{
		{
			name:      "interceptors run in declared order",
			wantOrder: []string{"first-before", "second-before", "third-before", "handler", "third-after", "second-after", "first-after"},
		},
		{
			name:      "error short circuits inner interceptors",
			failFirst: true,
			wantOrder: []string{"first-before"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order []string

			recorder := func(name string, fail bool) grpc.UnaryServerInterceptor {
				return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
					order = append(order, name+"-before")

					if fail {
						return nil, errors.New("interceptor failed")
					}

					resp, err := handler(ctx, req)

					order = append(order, name+"-after")

					return resp, err
				}
			}

			chain := middleware.ChainUnaryInterceptors(
				recorder("first", tt.failFirst),
				recorder("second", false),
				recorder("third", false),
			)

			_, err := chain(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
				order = append(order, "handler")
				return "done", nil
			})

			assert.Equal(t, tt.wantOrder, order)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
