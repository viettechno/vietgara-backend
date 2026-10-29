package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestPostgresSettings_Get(t *testing.T) {
	t.Parallel()

	name := "Gara Minh"
	repo := repository.NewPostgresSettings(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("FROM garage_notification_settings", dbtest.Row(&name, []byte(`{"vehicle_ready":false,"low_stock":true}`))).
		ExpectQueryRow("FROM garage_notification_settings", dbtest.Row(nil, []byte(`{}`))).
		ExpectQueryRow("FROM garage_notification_settings", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM garage_notification_settings", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("FROM garage_notification_settings", dbtest.Row(nil, []byte(`nope`)))
	ctx := tx.Context(t.Context())

	settings, err := repo.Get(ctx, "g-1")
	require.NoError(t, err)
	assert.Equal(t, "Gara Minh", settings.SenderName)
	assert.False(t, settings.Enabled(domain.TypeVehicleReady))
	assert.True(t, settings.Enabled(domain.TypeLowStock), "an explicit true is on")

	settings, err = repo.Get(ctx, "g-1")
	require.NoError(t, err)
	assert.Empty(t, settings.SenderName)
	assert.True(t, settings.Enabled(domain.TypeVehicleReady))

	settings, err = repo.Get(ctx, "g-1")
	require.NoError(t, err, "a garage with no row has the defaults")
	assert.True(t, settings.Enabled(domain.TypeQuoteReady))

	_, err = repo.Get(ctx, "g-1")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.Get(ctx, "g-1")
	require.ErrorContains(t, err, "decode notification settings")
}

func TestPostgresSettings_Save(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresSettings(nil)

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO garage_notification_settings", dbtest.Tag(1), nil).
		WithArgs(func(args []any) {
			assert.Equal(t, "g-1", args[0])
			assert.Equal(t, "Gara", *(args[1].(*string))) //nolint:forcetypeassert // nullableString
			assert.JSONEq(t, `{"vehicle_ready":false}`, string(args[2].([]byte)))
		}).
		ExpectExec("INSERT INTO garage_notification_settings", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.Save(ctx, "g-1", domain.Settings{SenderName: "Gara", Off: map[domain.Type]bool{domain.TypeVehicleReady: true}}))
	require.ErrorIs(t, repo.Save(ctx, "g-1", domain.Settings{}), errBoom)
}

func TestPostgresSuppressions(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresSuppressions(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("FROM email_suppressions", dbtest.Row(true)).
		ExpectQueryRow("FROM email_suppressions", dbtest.ErrRow(errBoom)).
		ExpectExec("INSERT INTO email_suppressions", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO email_suppressions", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	suppressed, err := repo.Suppressed(ctx, "Bad@example.com")
	require.NoError(t, err)
	assert.True(t, suppressed)

	_, err = repo.Suppressed(ctx, "x@example.com")
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.Suppress(ctx, "bad@example.com", "bounce"))
	require.ErrorIs(t, repo.Suppress(ctx, "bad@example.com", "bounce"), errBoom)
}

func TestPostgresStaff_Recipients(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresStaff(nil)

	tx := dbtest.New(t).
		ExpectQuery("FROM users u", dbtest.NewRows(
			[]any{"u-1", "lan@example.com", "Lan", "vi"}, []any{"u-2", "minh@example.com", "Minh", "en"},
		), nil).
		WithArgs(func(args []any) {
			assert.Equal(t, "g-1", args[0])
			assert.Equal(t, []string{"u-1"}, args[1])
			assert.Equal(t, "inventory.write", args[2])
		}).
		ExpectQuery("FROM users u", nil, errBoom).
		ExpectQuery("FROM users u", dbtest.NewRows([]any{"u-1", "a", "b", "vi"}).WithScanError(errBoom), nil).
		ExpectQuery("FROM users u", dbtest.NewRows([]any{"u-1", "a", "b", "vi"}).WithErr(errBoom), nil).
		ExpectQuery("FROM users u", dbtest.NewRows(), nil).
		WithArgs(func(args []any) { assert.Equal(t, []string{}, args[1], "no creator is an empty list, not null") })
	ctx := tx.Context(t.Context())

	users, err := repo.Recipients(ctx, "g-1", []string{"u-1"}, "inventory.write")
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, "en", users[1].Locale)

	for range 3 {
		_, err = repo.Recipients(ctx, "g-1", nil, "")
		require.ErrorIs(t, err, errBoom)
	}

	users, err = repo.Recipients(ctx, "g-1", nil, "")
	require.NoError(t, err)
	assert.Empty(t, users)
}
