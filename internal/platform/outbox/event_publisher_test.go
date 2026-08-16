package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// TestEventPublisher_Publish covers the outbox write path: SQL shape,
// payload marshalling, transaction-aware querier selection, and error
// propagation.
func TestEventPublisher_Publish(t *testing.T) {
	t.Run("writes a pending event to the outbox table", func(t *testing.T) {
		conn := mocks.NewMockConnPool(t)
		pool := platformdb.NewPool(t.Context(), conn)
		p := outbox.NewEventPublisher(pool)

		var capturedSQL string

		var capturedArgs []any

		conn.EXPECT().
			Exec(mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, sql string, args ...any) {
				capturedSQL = sql
				capturedArgs = args
			}).
			Return(pgconn.CommandTag{}, nil).
			Once()

		err := p.Publish(t.Context(), "garage.created", "garage-1", map[string]string{
			"garage_id":  "garage-1",
			"account_id": "acc-1",
		})

		require.NoError(t, err)
		assert.Contains(t, capturedSQL, "INSERT INTO outbox_events")
		require.Len(t, capturedArgs, 4)
		assert.Equal(t, "garage.created", capturedArgs[1])
		assert.Equal(t, "garage-1", capturedArgs[2])

		rawPayload, ok := capturedArgs[3].([]byte)
		require.True(t, ok)

		var payload map[string]string

		unmarshalErr := json.Unmarshal(rawPayload, &payload)
		require.NoError(t, unmarshalErr)

		assert.Equal(t, "garage-1", payload["garage_id"])
		assert.Equal(t, "acc-1", payload["account_id"])
	})

	t.Run("uses the active transaction from the context", func(t *testing.T) {
		conn := mocks.NewMockConnPool(t)
		tx := mocks.NewMockTx(t)
		pool := platformdb.NewPool(t.Context(), conn)
		p := outbox.NewEventPublisher(pool)
		txCtx := context.WithValue(t.Context(), platformdb.TxKey{}, tx)

		tx.EXPECT().
			Exec(mock.Anything, mock.Anything, mock.Anything).
			Return(pgconn.CommandTag{}, nil).
			Once()

		err := p.Publish(txCtx, "garage.created", "garage-1", map[string]string{"garage_id": "garage-1"})

		require.NoError(t, err)
		conn.AssertNotCalled(t, "Exec", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("returns the exec error", func(t *testing.T) {
		conn := mocks.NewMockConnPool(t)
		pool := platformdb.NewPool(t.Context(), conn)
		p := outbox.NewEventPublisher(pool)

		execErr := errors.New("database unavailable")

		conn.EXPECT().
			Exec(mock.Anything, mock.Anything, mock.Anything).
			Return(pgconn.CommandTag{}, execErr).
			Once()

		err := p.Publish(t.Context(), "garage.created", "garage-1", map[string]string{"garage_id": "garage-1"})

		require.ErrorIs(t, err, execErr)
	})

	t.Run("rejects unmarshalable payloads without touching the database", func(t *testing.T) {
		conn := mocks.NewMockConnPool(t)
		pool := platformdb.NewPool(t.Context(), conn)
		p := outbox.NewEventPublisher(pool)

		err := p.Publish(t.Context(), "garage.created", "garage-1", func() {})

		require.Error(t, err)
		conn.AssertNotCalled(t, "Exec", mock.Anything, mock.Anything, mock.Anything)
	})
}
