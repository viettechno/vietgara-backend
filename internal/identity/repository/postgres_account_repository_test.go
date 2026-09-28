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

func accountRow() []any {
	return []any{"acc-1", "owner@example.com", "0901234567", "google", "g-sub", "Minh", nil, "active", now, "en", true, now, now}
}

func TestFinders(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresAccountRepository(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("a.id = $1", dbtest.Row(accountRow()...)).
		ExpectQueryRow("lower(a.email) = lower($1)", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("a.phone = ANY($1)", dbtest.ErrRow(errBoom)).WithArgs(func(args []any) {
		assert.Equal(t, []string{"0901234567", "+84901234567"}, args[0])
	}).
		ExpectQueryRow("a.identity_platform_uid = $1", dbtest.Row(accountRow()...))
	ctx := tx.Context(t.Context())

	acc, err := repo.FindByID(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, domain.AuthProviderGoogle, acc.AuthProvider)
	assert.Equal(t, domain.LocaleEN, acc.Locale)
	assert.True(t, acc.PlatformAdmin)
	assert.True(t, acc.EmailVerified())
	assert.Equal(t, "g-sub", *acc.GoogleSubject)
	assert.Nil(t, acc.PasswordHash)

	_, err = repo.FindByEmail(ctx, " owner@example.com ")
	require.ErrorIs(t, err, domain.ErrAccountNotFound)

	_, err = repo.FindByPhone(ctx, "0901234567")
	require.ErrorIs(t, err, errBoom)

	_, err = repo.FindByGoogleSubject(ctx, "g-sub")
	require.NoError(t, err)
}

func TestFindByIDs(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresAccountRepository(nil)

	accounts, err := repo.FindByIDs(t.Context(), nil)
	require.NoError(t, err)
	assert.Nil(t, accounts)

	tx := dbtest.New(t).
		ExpectQuery("a.id = ANY($1)", dbtest.NewRows(accountRow()), nil).
		ExpectQuery("a.id = ANY($1)", nil, errBoom).
		ExpectQuery("a.id = ANY($1)", dbtest.NewRows([]any{"short"}), nil)
	ctx := tx.Context(t.Context())

	accounts, err = repo.FindByIDs(ctx, []string{"acc-1"})
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	_, err = repo.FindByIDs(ctx, []string{"x"})
	require.ErrorIs(t, err, errBoom)

	_, err = repo.FindByIDs(ctx, []string{"x"})
	require.Error(t, err)
}

func TestCreateAndUpdate(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresAccountRepository(nil)
	acc := &domain.Account{Email: "a@b.vn", AuthProvider: domain.AuthProviderEmail, FullName: "A", Status: domain.AccountStatusActive}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO accounts", dbtest.Row("acc-9", now, now)).WithArgs(func(args []any) {
		assert.Equal(t, "vi", args[8], "default locale")
	}).
		ExpectQueryRow("INSERT INTO accounts", dbtest.ErrRow(dbtest.UniqueViolation("idx_accounts_email_lower_unique"))).
		ExpectQueryRow("INSERT INTO accounts", dbtest.ErrRow(dbtest.UniqueViolation("accounts_phone_key"))).
		ExpectQueryRow("INSERT INTO accounts", dbtest.ErrRow(dbtest.UniqueViolation("accounts_pkey"))).
		ExpectQueryRow("UPDATE accounts", dbtest.Row(now)).
		ExpectQueryRow("UPDATE accounts", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectExec("INSERT INTO platform_admins", dbtest.Tag(2), nil).
		ExpectExec("INSERT INTO platform_admins", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.Create(ctx, acc))
	assert.Equal(t, "acc-9", acc.ID)
	require.ErrorIs(t, repo.Create(ctx, acc), domain.ErrEmailAlreadyExists)
	require.ErrorIs(t, repo.Create(ctx, acc), domain.ErrPhoneAlreadyExists)
	require.Error(t, repo.Create(ctx, acc))

	require.NoError(t, repo.Update(ctx, acc))
	require.ErrorIs(t, repo.Update(ctx, acc), domain.ErrAccountNotFound)

	n, err := repo.GrantPlatformAdmins(ctx, []string{"a@b.vn", "c@d.vn"})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = repo.GrantPlatformAdmins(ctx, []string{"x"})
	require.ErrorIs(t, err, errBoom)
}
