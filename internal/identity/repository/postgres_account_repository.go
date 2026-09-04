// Package repository implements the persistence layer of the Identity
// module on top of the shared Postgres pool.
// See: Database Design Section 2.1, Coding_Standards.docx Section 2.1.
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresAccountRepository implements AccountRepository against Cloud SQL
// for PostgreSQL. The accounts table has no garage_id and no RLS policy
// (see db/migrations/000001_create_accounts_table.up.sql), so queries here
// never need a tenant context.
type PostgresAccountRepository struct {
	pool *platformdb.Pool
}

// NewPostgresAccountRepository returns a PostgresAccountRepository backed
// by the given pool.
func NewPostgresAccountRepository(pool *platformdb.Pool) *PostgresAccountRepository {
	return &PostgresAccountRepository{pool: pool}
}

const accountColumns = `id, email, phone, auth_provider, identity_platform_uid, full_name, password_hash, status, created_at, updated_at`

func scanAccount(row pgx.Row) (*domain.Account, error) {
	var acc domain.Account

	var authProvider, status string

	err := row.Scan(&acc.ID, &acc.Email, &acc.Phone, &authProvider, &acc.IdentityPlatformUID, &acc.FullName, &acc.PasswordHash, &status, &acc.CreatedAt, &acc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAccountNotFound
		}

		return nil, err
	}

	acc.AuthProvider = domain.AuthProvider(authProvider)
	acc.Status = domain.AccountStatus(status)

	return &acc, nil
}

// FindByID returns the account with the given id.
func (r *PostgresAccountRepository) FindByID(ctx context.Context, id string) (*domain.Account, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = $1`, id)

	return scanAccount(row)
}

// FindByEmail returns the account with the given email.
func (r *PostgresAccountRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE email = $1`, email)

	return scanAccount(row)
}

// FindByPhone returns the account with the given phone number.
func (r *PostgresAccountRepository) FindByPhone(ctx context.Context, phone string) (*domain.Account, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE phone = $1`, phone)

	return scanAccount(row)
}

// FindByIdentityPlatformUID returns the account linked to the given GCP
// Identity Platform UID.
func (r *PostgresAccountRepository) FindByIdentityPlatformUID(ctx context.Context, uid string) (*domain.Account, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE identity_platform_uid = $1`, uid)

	return scanAccount(row)
}

// Create inserts the account, assigning id, created_at and updated_at from
// the database.
func (r *PostgresAccountRepository) Create(ctx context.Context, acc *domain.Account) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `
		INSERT INTO accounts (email, phone, auth_provider, identity_platform_uid, full_name, password_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		acc.Email, acc.Phone, string(acc.AuthProvider), acc.IdentityPlatformUID, acc.FullName, acc.PasswordHash, string(acc.Status),
	)

	return row.Scan(&acc.ID, &acc.CreatedAt, &acc.UpdatedAt)
}

// UpdatePasswordHash replaces the bcrypt hash of the account's password.
func (r *PostgresAccountRepository) UpdatePasswordHash(ctx context.Context, accountID, hash string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE accounts SET password_hash = $1 WHERE id = $2`, hash, accountID)

	return err
}
