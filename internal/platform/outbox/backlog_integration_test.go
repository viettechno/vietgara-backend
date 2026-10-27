//go:build integration

package outbox_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

func TestReadBacklog(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	pool := pg.AppPool(t)

	backlog, err := outbox.ReadBacklog(t.Context(), pool)
	require.NoError(t, err)
	assert.Equal(t, outbox.Backlog{}, backlog, "an empty outbox has no backlog")

	insert := func(status, age string) {
		_, err := pg.Admin.ExecContext(t.Context(), `
			INSERT INTO outbox_events (id, event_type, payload, status, created_at)
			VALUES ($1, 'garage.created', '{}', $2::outbox_status, now() - $3::interval)`, uuid.NewString(), status, age)
		require.NoError(t, err)
	}

	insert("pending", "90 seconds")
	insert("pending", "5 seconds")
	insert("published", "2 hours")
	insert("failed", "1 hour")

	backlog, err = outbox.ReadBacklog(t.Context(), pool)
	require.NoError(t, err)
	assert.EqualValues(t, 2, backlog.Pending)
	assert.EqualValues(t, 1, backlog.Failed)
	assert.InDelta(t, (90 * time.Second).Seconds(), backlog.OldestPending.Seconds(), 5)
}
