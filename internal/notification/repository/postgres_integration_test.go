//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/repository"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

type garageTx struct{ tx *platformdb.TxManager }

func (g garageTx) InGarage(ctx context.Context, garageID string, fn func(ctx context.Context) error) error {
	return g.tx.WithTransaction(ctx, platformdb.GarageScope(garageID), fn)
}

// TestNotificationStoresUnderRowLevelSecurity runs the settings, the log and the
// staff query as the RLS-bound application role: a garage sees its own rows and
// never another's, a failed attempt survives the rollback of the attempt, and the
// account e-mails stay out of the garage's log.
func TestNotificationStoresUnderRowLevelSecurity(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	txm := platformdb.NewTxManager(pool)
	logs := repository.NewPostgresRepository(pool).WithFailureTx(garageTx{tx: txm})
	settings := repository.NewPostgresSettings(pool)
	suppressions := repository.NewPostgresSuppressions(pool)
	staff := repository.NewPostgresStaff(pool)

	owner, member, garageA, garageB := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, u := range [][2]string{{owner, "owner@example.com"}, {member, "member@example.com"}} {
		_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name, locale) VALUES ($1, $2, 'Name', 'en')`, u[0], u[1])
		require.NoError(t, err)
	}

	for _, id := range []string{garageA, garageB} {
		_, err := pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, $3)`, id, owner, "garage-"+id[:8])
		require.NoError(t, err)
	}

	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO garage_staff (garage_id, user_id) VALUES ($1, $2)`, garageA, member)
	require.NoError(t, err)

	in := func(garageID string, fn func(ctx context.Context)) {
		t.Helper()
		require.NoError(t, txm.WithTransaction(ctx, platformdb.GarageScope(garageID), func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
	}

	in(garageA, func(ctx context.Context) {
		require.NoError(t, logs.Create(ctx, &domain.Log{
			ID: uuid.NewString(), Channel: domain.ChannelEmail, Template: "vehicle_ready", Recipient: "an@example.com",
			Status: domain.StatusSent, GarageID: garageA, Type: domain.TypeVehicleReady, EntityType: "repair_order", EntityID: uuid.NewString(),
		}))
		require.NoError(t, logs.Create(ctx, &domain.Log{
			ID: uuid.NewString(), Channel: domain.ChannelEmail, Template: "vehicle_ready", Recipient: "binh@example.com",
			Status: domain.StatusSkipped, SkipReason: domain.SkipOptedOut, GarageID: garageA, Type: domain.TypeVehicleReady,
		}))
		// An account e-mail: no garage.
		require.NoError(t, logs.Create(ctx, &domain.Log{
			ID: uuid.NewString(), UserID: owner, Channel: domain.ChannelEmail, Template: "password_reset", Recipient: "owner@example.com", Status: domain.StatusSent,
		}))

		rows, total, err := logs.List(ctx, garageA, usecase.LogFilter{}, 1, 20)
		require.NoError(t, err)
		assert.Equal(t, 2, total, "the account e-mail is not in the garage's log")
		require.Len(t, rows, 2)

		rows, total, err = logs.List(ctx, garageA, usecase.LogFilter{Status: domain.StatusSkipped}, 1, 20)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, domain.SkipOptedOut, rows[0].SkipReason)
	})

	in(garageB, func(ctx context.Context) {
		rows, total, err := logs.List(ctx, garageB, usecase.LogFilter{}, 1, 20)
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, rows)

		rows, total, err = logs.List(ctx, garageA, usecase.LogFilter{}, 1, 20)
		require.NoError(t, err)
		assert.Zero(t, total, "garage B cannot read garage A's log even when it asks for it")
		assert.Empty(t, rows)
	})

	// A failed attempt is written apart from the transaction that fails.
	failure := errBoom
	err = txm.WithTransaction(ctx, platformdb.GarageScope(garageA), func(ctx context.Context) error {
		require.NoError(t, logs.Failed(ctx, &domain.Log{
			ID: uuid.NewString(), Channel: domain.ChannelEmail, Template: "quote_decided", Recipient: "lan@example.com",
			Status: domain.StatusFailed, Error: "smtp down", GarageID: garageA, Type: domain.TypeQuoteDecided,
		}))

		return failure // the attempt rolls back
	})
	require.ErrorIs(t, err, errBoom)

	in(garageA, func(ctx context.Context) {
		rows, _, err := logs.List(ctx, garageA, usecase.LogFilter{Status: domain.StatusFailed}, 1, 20)
		require.NoError(t, err)
		require.Len(t, rows, 1, "the failure outlived the rollback")
		assert.Equal(t, "smtp down", rows[0].Error)
	})

	// Settings are per garage.
	in(garageA, func(ctx context.Context) {
		require.NoError(t, settings.Save(ctx, garageA, domain.Settings{SenderName: "Gara A", Off: map[domain.Type]bool{domain.TypeLowStock: true}}))
		got, err := settings.Get(ctx, garageA)
		require.NoError(t, err)
		assert.Equal(t, "Gara A", got.SenderName)
		assert.False(t, got.Enabled(domain.TypeLowStock))
		assert.True(t, got.Enabled(domain.TypeQuoteReady))
	})
	in(garageB, func(ctx context.Context) {
		got, err := settings.Get(ctx, garageB)
		require.NoError(t, err)
		assert.True(t, got.Enabled(domain.TypeLowStock), "garage B has its own defaults")
	})

	// Suppression is across garages and case-insensitive.
	require.NoError(t, suppressions.Suppress(ctx, "Bounced@Example.com", "bounce"))
	require.NoError(t, suppressions.Suppress(ctx, "bounced@example.com", "spam"), "the first reason is kept")

	suppressed, err := suppressions.Suppressed(ctx, "BOUNCED@example.com")
	require.NoError(t, err)
	assert.True(t, suppressed)

	suppressed, err = suppressions.Suppressed(ctx, "fine@example.com")
	require.NoError(t, err)
	assert.False(t, suppressed)

	// Recipients: the owner, plus the creator and the permission holders who are active staff.
	in(garageA, func(ctx context.Context) {
		users, err := staff.Recipients(ctx, garageA, []string{member}, "")
		require.NoError(t, err)
		require.Len(t, users, 2)
		assert.Equal(t, "member@example.com", users[0].Email)
		assert.Equal(t, "en", users[0].Locale)

		users, err = staff.Recipients(ctx, garageA, nil, "inventory.write")
		require.NoError(t, err)
		require.Len(t, users, 1, "only the owner: the member's role grants nothing")
		assert.Equal(t, "owner@example.com", users[0].Email)

		users, err = staff.Recipients(ctx, garageA, []string{uuid.NewString()}, "")
		require.NoError(t, err)
		require.Len(t, users, 1, "a user who is no staff of the garage is left out")
	})
}
