package middleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const testRoute = "/vietgara.tenant.v1.GarageService/GetGarage"

type garageScopedReq struct {
	garageID string
}

func (r garageScopedReq) GetGarageId() string {
	return r.garageID
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
			t.Parallel()

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

// garageAuthedCtx builds the context produced by AuthInterceptor followed
// by TenantContextInterceptor for an account that has access to garageID.
func garageAuthedCtx(t *testing.T, garageID string) context.Context {
	t.Helper()

	issuer := newTestIssuer(t)
	authed := authedCtx(t, issuer, issueToken(t, issuer, "acc-1", []auth.GarageClaim{{GarageID: garageID, Role: "owner"}}))

	gotCtx := make(chan context.Context, 1)
	_, err := middleware.TenantContextInterceptor()(authed, garageScopedReq{garageID: garageID}, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
		gotCtx <- ctx

		return nil, nil
	})
	require.NoError(t, err)

	return <-gotCtx
}

func TestTransactionInterceptor(t *testing.T) {
	t.Run("passes requests without a garage context through untransacted", func(t *testing.T) {
		connPool := mocks.NewMockConnPool(t)
		txManager := platformdb.NewTxManager(platformdb.NewPool(t.Context(), connPool))
		interceptor := middleware.TransactionInterceptor(txManager)

		got, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
			return "handled", nil
		})

		require.NoError(t, err)
		assert.Equal(t, "handled", got)
		connPool.AssertNotCalled(t, "Begin", mock.Anything)
	})

	t.Run("runs garage-scoped requests inside a tenant-scoped transaction", func(t *testing.T) {
		ctx := garageAuthedCtx(t, "garage-1")
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		pool := platformdb.NewPool(t.Context(), connPool)
		txManager := platformdb.NewTxManager(pool)
		interceptor := middleware.TransactionInterceptor(txManager)

		connPool.EXPECT().Begin(ctx).Return(tx, nil).Once()

		tx.EXPECT().Exec(ctx, platformdb.SetTenantSQL, []any{"garage-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Commit(ctx).Return(nil).Once()

		tx.EXPECT().Rollback(ctx).Return(pgx.ErrTxClosed).Once()

		handlerCtx := make(chan context.Context, 1)
		got, err := interceptor(ctx, garageScopedReq{garageID: "garage-1"}, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
			handlerCtx <- ctx

			return "handled", nil
		})

		require.NoError(t, err)
		assert.Equal(t, "handled", got)

		txCtx := <-handlerCtx
		assert.Equal(t, tx, platformdb.QuerierFromContext(txCtx, pool))
	})

	t.Run("rolls back and surfaces the handler error", func(t *testing.T) {
		ctx := garageAuthedCtx(t, "garage-1")
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		txManager := platformdb.NewTxManager(platformdb.NewPool(t.Context(), connPool))
		interceptor := middleware.TransactionInterceptor(txManager)
		handlerErr := errors.New("handler failed")

		connPool.EXPECT().Begin(ctx).Return(tx, nil).Once()

		tx.EXPECT().Exec(ctx, platformdb.SetTenantSQL, []any{"garage-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Rollback(ctx).Return(nil).Once()

		got, err := interceptor(ctx, garageScopedReq{garageID: "garage-1"}, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
			return nil, handlerErr
		})

		require.ErrorIs(t, err, handlerErr)
		assert.Nil(t, got)
		tx.AssertNotCalled(t, "Commit", ctx)
	})

	t.Run("surfaces transaction begin failures", func(t *testing.T) {
		ctx := garageAuthedCtx(t, "garage-1")
		connPool := mocks.NewMockConnPool(t)
		txManager := platformdb.NewTxManager(platformdb.NewPool(t.Context(), connPool))
		interceptor := middleware.TransactionInterceptor(txManager)
		handlerCalled := false

		connPool.EXPECT().Begin(ctx).Return(nil, errors.New("connection lost")).Once()

		got, err := interceptor(ctx, garageScopedReq{garageID: "garage-1"}, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
			handlerCalled = true

			return nil, nil
		})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.False(t, handlerCalled)
	})
}
