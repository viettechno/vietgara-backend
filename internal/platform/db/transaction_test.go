package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/mocks"
)

func TestNewTxManager(t *testing.T) {
	connPool := mocks.NewMockConnPool(t)
	pool := db.NewPool(t.Context(), connPool)

	m := db.NewTxManager(pool)

	require.NotNil(t, m)
}

func TestTxManager_WithTransaction(t *testing.T) {
	garageID := "garage-1"
	beginErr := errors.New("connection lost")
	setTenantErr := errors.New("database unavailable")
	callbackErr := errors.New("callback failed")
	commitErr := errors.New("commit failed")

	tests := []struct {
		name         string
		garageID     *string
		beginErr     error
		setTenantErr error
		callbackErr  error
		commitErr    error
		callback     func(t *testing.T, ctx context.Context, pool *db.Pool, tx db.Querier) error
		wantErr      string
		wantErrIs    error
		wantCallback bool
	}{
		{
			name:     "commits and scopes the callback to the tenant",
			garageID: &garageID,
			callback: func(t *testing.T, ctx context.Context, pool *db.Pool, tx db.Querier) error {
				t.Helper()

				require.Equal(t, tx, db.QuerierFromContext(ctx, pool))

				return nil
			},
			wantCallback: true,
		},
		{
			name: "skips the tenant variable when garageID is nil",
			callback: func(_ *testing.T, _ context.Context, _ *db.Pool, _ db.Querier) error {
				return nil
			},
			wantCallback: true,
		},
		{
			name:      "wraps begin errors without invoking the callback",
			beginErr:  beginErr,
			wantErrIs: db.ErrBeginTransaction,
		},
		{
			name:         "wraps tenant context errors without invoking the callback",
			garageID:     &garageID,
			setTenantErr: setTenantErr,
			wantErr:      "set tenant context: database unavailable",
		},
		{
			name:         "returns the callback error and rolls back",
			garageID:     &garageID,
			callbackErr:  callbackErr,
			wantErrIs:    callbackErr,
			wantCallback: true,
		},
		{
			name:         "wraps commit failures",
			garageID:     &garageID,
			commitErr:    commitErr,
			wantErr:      "commit transaction: commit failed",
			wantCallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connPool := mocks.NewMockConnPool(t)
			tx := mocks.NewMockTx(t)
			pool := db.NewPool(t.Context(), connPool)
			m := db.NewTxManager(pool)
			ctx := t.Context()

			connPool.EXPECT().Begin(ctx).Return(tx, tt.beginErr)

			if tt.beginErr == nil {
				if tt.garageID != nil {
					tx.EXPECT().Exec(ctx, db.SetTenantSQL, []any{*tt.garageID}).Return(pgconn.CommandTag{}, tt.setTenantErr)
				}

				if tt.setTenantErr == nil && tt.callbackErr == nil {
					tx.EXPECT().Commit(ctx).Return(tt.commitErr)
				}

				var rollbackErr error
				if tt.setTenantErr == nil && tt.callbackErr == nil && tt.commitErr == nil {
					rollbackErr = pgx.ErrTxClosed
				}

				tx.EXPECT().Rollback(ctx).Return(rollbackErr)
			}

			callbackCalled := false

			err := m.WithTransaction(ctx, tt.garageID, func(ctx context.Context) error {
				callbackCalled = true

				if tt.callback != nil {
					return tt.callback(t, ctx, pool, tx)
				}

				return tt.callbackErr
			})

			switch {
			case tt.setTenantErr != nil:
				require.ErrorContains(t, err, tt.wantErr)
			case tt.callbackErr != nil:
				require.ErrorIs(t, err, tt.wantErrIs)
			case tt.commitErr != nil:
				require.ErrorContains(t, err, tt.wantErr)
			case tt.beginErr != nil:
				require.ErrorIs(t, err, tt.wantErrIs)
			default:
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantCallback, callbackCalled)
		})
	}
}
