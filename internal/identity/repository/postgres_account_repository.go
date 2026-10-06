// Package repository implements the persistence layer of the Identity
// module: accounts on the shared Postgres pool, and sessions and OTPs in
// Redis (Memorystore) or, for a single local instance, in memory.
// See: Database Design Section 2.1, Security Architecture Section 2.2.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresAccountRepository implements usecase.AccountRepository. The
// accounts table has no garage_id and no RLS policy (it sits above the
// tenant level), so queries here never need a tenant context.
type PostgresAccountRepository struct {
	pool *platformdb.Pool
}

// NewPostgresAccountRepository returns a PostgresAccountRepository backed
// by the given pool.
func NewPostgresAccountRepository(pool *platformdb.Pool) *PostgresAccountRepository {
	return &PostgresAccountRepository{pool: pool}
}

const accountColumns = `a.id, COALESCE(a.email, ''), a.phone, a.auth_provider, a.identity_platform_uid, a.full_name,
	a.password_hash, a.status, a.email_verified_at, a.locale,
	EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.account_id = a.id), a.created_at, a.updated_at`

func scanAccount(row pgx.Row) (*domain.Account, error) {
	var (
		acc                          domain.Account
		authProvider, status, locale string
	)

	err := row.Scan(&acc.ID, &acc.Email, &acc.Phone, &authProvider, &acc.GoogleSubject, &acc.FullName,
		&acc.PasswordHash, &status, &acc.EmailVerifiedAt, &locale, &acc.PlatformAdmin, &acc.CreatedAt, &acc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAccountNotFound
		}

		return nil, err
	}

	acc.AuthProvider = domain.AuthProvider(authProvider)
	acc.Status = domain.AccountStatus(status)
	acc.Locale = domain.Locale(locale)

	return &acc, nil
}

func (r *PostgresAccountRepository) findOne(ctx context.Context, where string, args ...any) (*domain.Account, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanAccount(q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a WHERE `+where+` LIMIT 1`, args...))
}

// FindByID returns the account with the given id.
func (r *PostgresAccountRepository) FindByID(ctx context.Context, id string) (*domain.Account, error) {
	return r.findOne(ctx, `a.id = $1`, id)
}

// FindByEmail matches the e-mail case-insensitively.
func (r *PostgresAccountRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	return r.findOne(ctx, `lower(a.email) = lower($1)`, strings.TrimSpace(email))
}

// FindByPhone matches a phone in either its "0..." or "+84..." form.
func (r *PostgresAccountRepository) FindByPhone(ctx context.Context, phone string) (*domain.Account, error) {
	return r.findOne(ctx, `a.phone = ANY($1)`, PhoneVariants(phone))
}

// FindByGoogleSubject returns the account linked to a Google user id.
func (r *PostgresAccountRepository) FindByGoogleSubject(ctx context.Context, subject string) (*domain.Account, error) {
	return r.findOne(ctx, `a.identity_platform_uid = $1`, subject)
}

// FindByIDs returns the accounts with the given ids (missing ones are
// skipped).
func (r *PostgresAccountRepository) FindByIDs(ctx context.Context, ids []string) ([]*domain.Account, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `SELECT `+accountColumns+` FROM accounts a WHERE a.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*domain.Account

	for rows.Next() {
		acc, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}

		accounts = append(accounts, acc)
	}

	return accounts, rows.Err()
}

// Create inserts acc and fills its id and timestamps.
func (r *PostgresAccountRepository) Create(ctx context.Context, acc *domain.Account) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO accounts (email, phone, auth_provider, identity_platform_uid, full_name, password_hash, status, email_verified_at, locale)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		acc.Email, acc.Phone, string(acc.AuthProvider), acc.GoogleSubject, acc.FullName, acc.PasswordHash,
		string(acc.Status), acc.EmailVerifiedAt, string(acc.Locale.OrDefault()),
	).Scan(&acc.ID, &acc.CreatedAt, &acc.UpdatedAt)

	return mapUniqueViolation(err)
}

// Update persists the mutable fields of acc.
func (r *PostgresAccountRepository) Update(ctx context.Context, acc *domain.Account) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE accounts
		SET full_name = $2, phone = $3, locale = $4, password_hash = $5, email_verified_at = $6, identity_platform_uid = $7
		WHERE id = $1
		RETURNING updated_at`,
		acc.ID, acc.FullName, acc.Phone, string(acc.Locale.OrDefault()), acc.PasswordHash, acc.EmailVerifiedAt, acc.GoogleSubject,
	).Scan(&acc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAccountNotFound
	}

	return mapUniqueViolation(err)
}

// GrantPlatformAdmins adds the accounts with the given e-mails to
// platform_admins.
func (r *PostgresAccountRepository) GrantPlatformAdmins(ctx context.Context, emails []string) (int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `
		INSERT INTO platform_admins (account_id)
		SELECT id FROM accounts WHERE lower(email) = ANY($1)
		ON CONFLICT DO NOTHING`, emails)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}

// mapUniqueViolation turns duplicate e-mail/phone errors into domain errors.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}

	switch {
	case strings.Contains(pgErr.ConstraintName, "email"):
		return domain.ErrEmailAlreadyExists
	case strings.Contains(pgErr.ConstraintName, "phone"):
		return domain.ErrPhoneAlreadyExists
	default:
		return err
	}
}

// DeleteTestAccounts implements usecase.TestAccountDeleter. Each account
// is deleted on its own, so one that cannot be removed (say, a test staff
// member who invited a real account: staff_invitations.invited_by does not
// cascade) does not keep the others; its error is returned with the rest.
func (r *PostgresAccountRepository) DeleteTestAccounts(ctx context.Context, emailDomain string, createdBefore time.Time) (int64, error) {
	if emailDomain == "" {
		return 0, errors.New("delete test accounts: empty e-mail domain")
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT id FROM accounts
		WHERE right(lower(email), length($1) + 1) = '@' || $1 AND created_at < $2`,
		strings.ToLower(emailDomain), createdBefore)
	if err != nil {
		return 0, fmt.Errorf("list test accounts: %w", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("list test accounts: %w", err)
	}

	var (
		deleted int64
		errs    []error
	)

	for _, id := range ids {
		// Garages, memberships, subscriptions and the rest cascade.
		if _, err := q.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id); err != nil {
			errs = append(errs, fmt.Errorf("delete test account %s: %w", id, err))

			continue
		}

		deleted++
	}

	return deleted, errors.Join(errs...)
}
