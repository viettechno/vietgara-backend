package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/idempotency"
)

// scheduleIdempotencyPurge deletes expired Idempotency-Keys now and then
// every idempotency.PurgeInterval until ctx is cancelled.
func scheduleIdempotencyPurge(ctx context.Context, store *idempotency.Store) {
	ticker := time.NewTicker(idempotency.PurgeInterval)
	defer ticker.Stop()

	for {
		if deleted, err := store.Purge(ctx); err != nil && ctx.Err() == nil {
			slog.Error("idempotency: purging expired keys failed", "error", err)
		} else if deleted > 0 {
			slog.InfoContext(ctx, "idempotency: purged expired keys", "count", deleted)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
