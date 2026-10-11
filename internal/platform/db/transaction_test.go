package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/db"
)

func TestScope(t *testing.T) {
	t.Parallel()

	assert.True(t, db.Scope{}.IsZero())
	assert.Equal(t, db.Scope{GarageID: "g1"}, db.GarageScope("g1"))
	assert.Equal(t, db.Scope{UserID: "a1"}, db.UserScope("a1"))
	assert.Equal(t, db.Scope{GarageID: "g2", UserID: "a1"}, db.UserScope("a1").Garage("g2"))
	assert.False(t, db.GarageScope("g1").IsZero())
}

func TestTxManager_WithTransaction(t *testing.T) {
	scope := db.Scope{GarageID: "garage-1", UserID: "user-1"}
	beginErr := errTransactionConnectionLost
	setScopeErr := errTransactionDatabaseUnavailable
	callbackErr := errTransactionCallbackFailed
	commitErr := errTransactionCommitFailed

	tests := []struct {
		name         string
		scope        db.Scope
		beginErr     error
		setScopeErr  error
		callbackErr  error
		commitErr    error
		wantErrIs    error
		wantCallback bool
	}{
		{name: "commits and scopes the callback", scope: scope, wantCallback: true},
		{name: "skips the session variables for an empty scope", wantCallback: true},
		{name: "wraps begin errors", beginErr: beginErr, wantErrIs: db.ErrBeginTransaction},
		{name: "wraps scope errors", scope: scope, setScopeErr: setScopeErr, wantErrIs: db.ErrSetTenantContext},
		{name: "returns the callback error", scope: scope, callbackErr: callbackErr, wantErrIs: callbackErr, wantCallback: true},
		{name: "wraps commit failures", scope: scope, commitErr: commitErr, wantErrIs: db.ErrCommitTransaction, wantCallback: true},
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
				if !tt.scope.IsZero() {
					tx.EXPECT().Exec(ctx, db.SetScopeSQL, []any{tt.scope.GarageID, tt.scope.UserID}).Return(pgconn.CommandTag{}, tt.setScopeErr)
				}

				if tt.setScopeErr == nil && tt.callbackErr == nil {
					tx.EXPECT().Commit(ctx).Return(tt.commitErr)
				}

				tx.EXPECT().Rollback(ctx).Return(pgx.ErrTxClosed)
			}

			called := false
			err := m.WithTransaction(ctx, tt.scope, func(ctx context.Context) error {
				called = true

				assert.True(t, db.InTransaction(ctx))
				assert.Equal(t, tx, db.QuerierFromContext(ctx, pool))
				assert.Equal(t, tt.scope, db.ScopeFromContext(ctx))

				return tt.callbackErr
			})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantCallback, called)
		})
	}
}

func TestTxManager_NestedTransactionsJoin(t *testing.T) {
	outerScope := db.UserScope("user-1")
	innerScope := outerScope.Garage("garage-1")

	t.Run("same or empty scope runs directly", func(t *testing.T) {
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		m := db.NewTxManager(db.NewPool(t.Context(), connPool))

		connPool.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"", "user-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Commit(mock.Anything).Return(nil)
		tx.EXPECT().Rollback(mock.Anything).Return(nil)

		err := m.WithTransaction(t.Context(), outerScope, func(ctx context.Context) error {
			require.NoError(t, m.WithTransaction(ctx, outerScope, func(context.Context) error { return nil }))

			return m.WithTransaction(ctx, db.Scope{}, func(context.Context) error { return nil })
		})
		require.NoError(t, err)
	})

	t.Run("different scope is switched and restored", func(t *testing.T) {
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		m := db.NewTxManager(db.NewPool(t.Context(), connPool))

		connPool.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"", "user-1"}).Return(pgconn.CommandTag{}, nil).Twice()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"garage-1", "user-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Commit(mock.Anything).Return(nil)
		tx.EXPECT().Rollback(mock.Anything).Return(nil)

		err := m.WithTransaction(t.Context(), outerScope, func(ctx context.Context) error {
			return m.WithTransaction(ctx, innerScope, func(ctx context.Context) error {
				assert.Equal(t, innerScope, db.ScopeFromContext(ctx))

				return nil
			})
		})
		require.NoError(t, err)
	})

	t.Run("switch and restore failures are reported", func(t *testing.T) {
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		m := db.NewTxManager(db.NewPool(t.Context(), connPool))
		failure := errTransactionBoom

		connPool.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"", "user-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"garage-1", "user-1"}).Return(pgconn.CommandTag{}, failure).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"garage-2", "user-1"}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Exec(mock.Anything, db.SetScopeSQL, []any{"", "user-1"}).Return(pgconn.CommandTag{}, failure).Once()
		tx.EXPECT().Rollback(mock.Anything).Return(nil)

		err := m.WithTransaction(t.Context(), outerScope, func(ctx context.Context) error {
			switchErr := m.WithTransaction(ctx, innerScope, func(context.Context) error { return nil })
			require.ErrorIs(t, switchErr, db.ErrSetTenantContext)

			return m.WithTransaction(ctx, outerScope.Garage("garage-2"), func(context.Context) error { return nil })
		})
		require.ErrorIs(t, err, db.ErrSetTenantContext)
	})
}

func TestAfterCommit(t *testing.T) {
	t.Run("runs immediately without a transaction", func(t *testing.T) {
		ran := false

		db.AfterCommit(t.Context(), func() { ran = true })
		assert.True(t, ran)
	})

	t.Run("runs after commit only", func(t *testing.T) {
		connPool := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		m := db.NewTxManager(db.NewPool(t.Context(), connPool))

		connPool.EXPECT().Begin(mock.Anything).Return(tx, nil).Twice()
		tx.EXPECT().Rollback(mock.Anything).Return(nil).Twice()
		tx.EXPECT().Commit(mock.Anything).Return(nil).Once()

		var calls []string

		err := m.WithTransaction(t.Context(), db.Scope{}, func(ctx context.Context) error {
			db.AfterCommit(ctx, func() { calls = append(calls, "committed") })
			assert.Empty(t, calls, "deferred until commit")

			return nil
		})
		require.NoError(t, err)

		err = m.WithTransaction(t.Context(), db.Scope{}, func(ctx context.Context) error {
			db.AfterCommit(ctx, func() { calls = append(calls, "rolled back") })

			return errTransactionFail
		})
		require.Error(t, err)
		assert.Equal(t, []string{"committed"}, calls)
	})
}
