package db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/db"
)

const (
	testUser = "user"
	testPass = "pass"
)

func validTestDSN() string {
	return fmt.Sprintf("postgres://%s:%s@localhost:5432/db?sslmode=disable", testUser, testPass)
}

func TestNewPgxPool(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr error
	}{
		{name: "valid dsn", dsn: validTestDSN()},
		{name: "malformed url", dsn: "://", wantErr: db.ErrParsePgxConfig},
		{name: "non-numeric pool_max_conns", dsn: validTestDSN() + "&pool_max_conns=abc", wantErr: db.ErrParsePgxConfig},
		{name: "too small pool_max_conns", dsn: validTestDSN() + "&pool_max_conns=0", wantErr: db.ErrParsePgxConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := db.NewPgxPool(t.Context(), tt.dsn)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pool)

			t.Cleanup(pool.Close)
		})
	}
}

func TestNewPool(t *testing.T) {
	connPool := mocks.NewMockConnPool(t)
	connPool.EXPECT().Close().Twice()

	pool := db.NewPool(t.Context(), connPool)
	pool.Close()
	pool.Close()
}

func TestQuerierFromContext(t *testing.T) {
	connPool := mocks.NewMockConnPool(t)
	activeTx := mocks.NewMockTx(t)

	tests := []struct {
		name string
		ctx  context.Context
		pool *db.Pool
		want db.Querier
	}{
		{
			name: "returns the active transaction",
			ctx:  context.WithValue(t.Context(), db.TxKey{}, activeTx),
			pool: db.NewPool(t.Context(), connPool),
			want: db.Querier(activeTx),
		},
		{
			name: "ignores a wrong context value type",
			ctx:  context.WithValue(t.Context(), db.TxKey{}, "not-a-tx"),
			pool: db.NewPool(t.Context(), connPool),
			want: connPool,
		},
		{
			name: "falls back to the pool without a transaction",
			ctx:  t.Context(),
			pool: db.NewPool(t.Context(), connPool),
			want: connPool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, db.QuerierFromContext(tt.ctx, tt.pool))
		})
	}
}
