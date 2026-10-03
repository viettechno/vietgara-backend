package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		log := &domain.Log{
			ID: "n-1", AccountID: "acc-1", Channel: domain.ChannelEmail, Template: "registration_confirmation",
			Recipient: "an@example.com", Status: domain.StatusSent,
		}

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO notification_log", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, log))
		require.Equal(t, now, log.CreatedAt)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		log := &domain.Log{ID: "n-1", Channel: domain.ChannelEmail, Template: "t", Recipient: "a@b.com", Status: domain.StatusSent}

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO notification_log", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, log), errBoom)
	})
}
