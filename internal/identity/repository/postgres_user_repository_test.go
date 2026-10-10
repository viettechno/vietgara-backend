package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
)

func userRow() []any {
	return []any{"user-1", "owner@example.com", "0901234567", "google", "g-sub", "Minh", nil, "active", now, "en", true, now, now}
}

func TestFinders(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresUserRepository(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("a.id = $1", dbtest.Row(userRow()...)).
		ExpectQueryRow("lower(a.email) = lower($1)", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("a.phone = ANY($1)", dbtest.ErrRow(errBoom)).WithArgs(func(args []any) {
		assert.Equal(t, []string{"0901234567", "+84901234567"}, args[0])
	}).
		ExpectQueryRow("a.identity_platform_uid = $1", dbtest.Row(userRow()...))
	ctx := tx.Context(t.Context())

	user, err := repo.FindByID(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, domain.AuthProviderGoogle, user.AuthProvider)
	assert.Equal(t, domain.LocaleEN, user.Locale)
	assert.True(t, user.PlatformAdmin)
	assert.True(t, user.EmailVerified())
	assert.Equal(t, "g-sub", *user.GoogleSubject)
	assert.Nil(t, user.PasswordHash)

	_, err = repo.FindByEmail(ctx, " owner@example.com ")
	require.ErrorIs(t, err, domain.ErrUserNotFound)

	_, err = repo.FindByPhone(ctx, "0901234567")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.FindByGoogleSubject(ctx, "g-sub")
	require.NoError(t, err)
}

func TestFindByIDs(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresUserRepository(nil)

	users, err := repo.FindByIDs(t.Context(), nil)
	require.NoError(t, err)
	assert.Nil(t, users)

	tx := dbtest.New(t).
		ExpectQuery("a.id = ANY($1)", dbtest.NewRows(userRow()), nil).
		ExpectQuery("a.id = ANY($1)", nil, errBoom).
		ExpectQuery("a.id = ANY($1)", dbtest.NewRows([]any{"short"}), nil)
	ctx := tx.Context(t.Context())

	users, err = repo.FindByIDs(ctx, []string{"user-1"})
	require.NoError(t, err)
	require.Len(t, users, 1)

	_, err = repo.FindByIDs(ctx, []string{"x"})
	require.ErrorIs(t, err, errBoom)

	_, err = repo.FindByIDs(ctx, []string{"x"})
	require.Error(t, err)
}

func TestCreateAndUpdate(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresUserRepository(nil)
	user := &domain.User{Email: "a@b.vn", AuthProvider: domain.AuthProviderEmail, FullName: "A", Status: domain.UserStatusActive}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO users", dbtest.Row("user-9", now, now)).WithArgs(func(args []any) {
		assert.Equal(t, "vi", args[8], "default locale")
	}).
		ExpectQueryRow("INSERT INTO users", dbtest.ErrRow(dbtest.UniqueViolation("idx_users_email_lower_unique"))).
		ExpectQueryRow("INSERT INTO users", dbtest.ErrRow(dbtest.UniqueViolation("users_phone_key"))).
		ExpectQueryRow("INSERT INTO users", dbtest.ErrRow(dbtest.UniqueViolation("users_pkey"))).
		ExpectQueryRow("UPDATE users", dbtest.Row(now)).
		ExpectQueryRow("UPDATE users", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectExec("INSERT INTO platform_admins", dbtest.Tag(2), nil).
		ExpectExec("INSERT INTO platform_admins", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.Create(ctx, user))
	assert.Equal(t, "user-9", user.ID)
	require.ErrorIs(t, repo.Create(ctx, user), domain.ErrEmailAlreadyExists)
	require.ErrorIs(t, repo.Create(ctx, user), domain.ErrPhoneAlreadyExists)
	require.Error(t, repo.Create(ctx, user))

	require.NoError(t, repo.Update(ctx, user))
	require.ErrorIs(t, repo.Update(ctx, user), domain.ErrUserNotFound)

	n, err := repo.GrantPlatformAdmins(ctx, []string{"a@b.vn", "c@d.vn"})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = repo.GrantPlatformAdmins(ctx, []string{"x"})
	require.ErrorIs(t, err, errBoom)
}
