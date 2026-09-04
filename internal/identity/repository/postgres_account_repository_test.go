package repository_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// ---------------------------------------------------------------------------
// Helpers driving the generated pgx.Row mocks
// ---------------------------------------------------------------------------

// assignScanValues copies values[i] into the i-th Scan destination, leaving
// nil values untouched so pointer fields stay nil.
func assignScanValues(dest, values []any) error {
	for i := range dest {
		value := values[i]

		if value == nil {
			continue
		}

		dv := reflect.ValueOf(dest[i]).Elem()
		sv := reflect.ValueOf(value)

		if !sv.Type().AssignableTo(dv.Type()) {
			return errors.New("scan: value type mismatch")
		}

		dv.Set(sv)
	}

	return nil
}

// errRow is a shortcut for a row whose Scan always fails (e.g. pgx.ErrNoRows).
func errRow(t *testing.T, err error) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).Return(err)

	return row
}

// valuesRow is a row whose Scan copies the given values into the caller's
// destinations (like a single real pgx result row).
func valuesRow(t *testing.T, values []any) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).Run(func(dest ...any) {
		require.NoError(t, assignScanValues(dest, values))
	}).Return(nil)

	return row
}

func sqlContaining(fragment string) any {
	return mock.MatchedBy(func(sql string) bool {
		return strings.Contains(sql, fragment)
	})
}

func newQuerier(t *testing.T) (*mocks.MockConnPool, *dbpkg.Pool) {
	t.Helper()

	conn := mocks.NewMockConnPool(t)
	pool := dbpkg.NewPool(t.Context(), conn)

	return conn, pool
}

// ---------------------------------------------------------------------------
// Column fixture
// ---------------------------------------------------------------------------

var testTimestamp = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func accountScanValues(id string, email, phone, uid, passwordHash *string, authProvider, fullName, status string) []any {
	return []any{id, email, phone, authProvider, uid, fullName, passwordHash, status, testTimestamp, testTimestamp}
}

func TestPostgresAccountRepository_FindByID(t *testing.T) {
	scanBoom := errors.New("scan boom")

	tests := []struct {
		name    string
		row     *mocks.MockRow
		want    *domain.Account
		wantErr error
	}{
		{
			name: "returns the account",
			row: valuesRow(t, accountScanValues(
				"acc-1", new("user@example.com"), nil, new("uid-1"), new("hash"), "email", "Test User", "active",
			)),
			want: &domain.Account{
				ID: "acc-1", Email: new("user@example.com"), AuthProvider: domain.AuthProviderEmail,
				IdentityPlatformUID: new("uid-1"), FullName: "Test User", PasswordHash: new("hash"),
				Status: domain.AccountStatusActive, CreatedAt: testTimestamp, UpdatedAt: testTimestamp,
			},
		},
		{
			name:    "maps ErrNoRows to ErrAccountNotFound",
			row:     errRow(t, pgx.ErrNoRows),
			wantErr: domain.ErrAccountNotFound,
		},
		{
			name:    "propagates scan errors",
			row:     errRow(t, scanBoom),
			wantErr: scanBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresAccountRepository(pool)

			conn.EXPECT().
				QueryRow(t.Context(), sqlContaining("FROM accounts WHERE id"), []any{"acc-1"}).
				Return(tt.row)

			got, err := repo.FindByID(t.Context(), "acc-1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPostgresAccountRepository_FindByEmail(t *testing.T) {
	scanBoom := errors.New("scan boom")

	tests := []struct {
		name      string
		email     string
		row       *mocks.MockRow
		wantEmail string
		wantErr   error
	}{
		{
			name:      "returns the account",
			email:     "user@example.com",
			row:       valuesRow(t, accountScanValues("acc-1", new("user@example.com"), nil, new("uid-1"), new("hash"), "email", "Test User", "active")),
			wantEmail: "user@example.com",
		},
		{
			name:    "unknown email maps to ErrAccountNotFound",
			email:   "nobody@example.com",
			row:     errRow(t, pgx.ErrNoRows),
			wantErr: domain.ErrAccountNotFound,
		},
		{
			name:    "propagates scan errors",
			email:   "user@example.com",
			row:     errRow(t, scanBoom),
			wantErr: scanBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresAccountRepository(pool)

			conn.EXPECT().
				QueryRow(t.Context(), sqlContaining("WHERE email ="), []any{tt.email}).
				Return(tt.row)

			got, err := repo.FindByEmail(t.Context(), tt.email)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got.Email)
			assert.Equal(t, tt.wantEmail, *got.Email)
		})
	}
}

func TestPostgresAccountRepository_FindByPhone(t *testing.T) {
	t.Run("returns the account", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("WHERE phone ="), []any{"+84901234567"}).
			Return(valuesRow(t, accountScanValues("acc-2", nil, new("+84901234567"), nil, nil, "email", "Phone User", "active")))

		got, err := repo.FindByPhone(t.Context(), "+84901234567")
		require.NoError(t, err)
		require.NotNil(t, got.Phone)
		assert.Equal(t, "+84901234567", *got.Phone)
	})

	t.Run("unknown phone maps to ErrAccountNotFound", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("WHERE phone ="), []any{"+84000000000"}).
			Return(errRow(t, pgx.ErrNoRows))

		_, err := repo.FindByPhone(t.Context(), "+84000000000")
		require.ErrorIs(t, err, domain.ErrAccountNotFound)
	})
}

func TestPostgresAccountRepository_FindByIdentityPlatformUID(t *testing.T) {
	t.Run("returns the account", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("WHERE identity_platform_uid ="), []any{"google-uid-1"}).
			Return(valuesRow(t, accountScanValues("acc-3", new("g@example.com"), nil, new("google-uid-1"), nil, "google", "G User", "active")))

		got, err := repo.FindByIdentityPlatformUID(t.Context(), "google-uid-1")
		require.NoError(t, err)
		require.NotNil(t, got.IdentityPlatformUID)
		assert.Equal(t, "google-uid-1", *got.IdentityPlatformUID)
	})

	t.Run("unknown uid maps to ErrAccountNotFound", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("WHERE identity_platform_uid ="), []any{"unknown"}).
			Return(errRow(t, pgx.ErrNoRows))

		_, err := repo.FindByIdentityPlatformUID(t.Context(), "unknown")
		require.ErrorIs(t, err, domain.ErrAccountNotFound)
	})
}

func TestPostgresAccountRepository_Create(t *testing.T) {
	t.Run("inserts the account and returns id and timestamps", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		acc := &domain.Account{
			Email:        new("new@example.com"),
			AuthProvider: domain.AuthProviderEmail,
			FullName:     "New User",
			PasswordHash: new("hash"),
			Status:       domain.AccountStatusActive,
		}

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("INSERT INTO accounts"), []any{
				acc.Email, acc.Phone, "email", acc.IdentityPlatformUID, acc.FullName, acc.PasswordHash, "active",
			}).
			Return(valuesRow(t, []any{"acc-new", testTimestamp, testTimestamp}))

		err := repo.Create(t.Context(), acc)
		require.NoError(t, err)
		assert.Equal(t, "acc-new", acc.ID)
		assert.Equal(t, testTimestamp, acc.CreatedAt)
		assert.Equal(t, testTimestamp, acc.UpdatedAt)
	})

	t.Run("propagates insert errors", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		acc := &domain.Account{Email: new("x@example.com"), AuthProvider: domain.AuthProviderEmail, FullName: "X", Status: domain.AccountStatusActive}

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("INSERT INTO accounts"), mock.Anything).
			Return(errRow(t, errors.New("insert boom")))

		err := repo.Create(t.Context(), acc)
		require.Error(t, err)
	})
}

func TestPostgresAccountRepository_UpdatePasswordHash(t *testing.T) {
	t.Run("updates the hash", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			Exec(t.Context(), sqlContaining("UPDATE accounts SET password_hash"), []any{"new-hash", "acc-1"}).
			Return(pgconn.CommandTag{}, nil)

		err := repo.UpdatePasswordHash(t.Context(), "acc-1", "new-hash")
		require.NoError(t, err)
	})

	t.Run("propagates update errors", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresAccountRepository(pool)

		conn.EXPECT().
			Exec(t.Context(), sqlContaining("UPDATE accounts SET password_hash"), mock.Anything).
			Return(pgconn.CommandTag{}, errors.New("update boom"))

		err := repo.UpdatePasswordHash(t.Context(), "acc-1", "new-hash")
		require.Error(t, err)
	})
}
