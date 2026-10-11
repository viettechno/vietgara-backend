// Package repository implements the persistence layer of the Identity
// module: users on the shared Postgres pool, and sessions and OTPs in
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

// PostgresUserRepository implements usecase.UserRepository. The
// users table has no garage_id and no RLS policy (it sits above the
// tenant level), so queries here never need a tenant context.
type PostgresUserRepository struct {
	pool *platformdb.Pool
}

// NewPostgresUserRepository returns a PostgresUserRepository backed
// by the given pool.
func NewPostgresUserRepository(pool *platformdb.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

const userColumns = `a.id, COALESCE(a.email, ''), a.phone, a.auth_provider, a.identity_platform_uid, a.full_name,
	a.password_hash, a.status, a.email_verified_at, a.locale,
	EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.user_id = a.id), a.created_at, a.updated_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var (
		user                         domain.User
		authProvider, status, locale string
	)

	err := row.Scan(&user.ID, &user.Email, &user.Phone, &authProvider, &user.GoogleSubject, &user.FullName,
		&user.PasswordHash, &status, &user.EmailVerifiedAt, &locale, &user.PlatformAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}

		return nil, err
	}

	user.AuthProvider = domain.AuthProvider(authProvider)
	user.Status = domain.UserStatus(status)
	user.Locale = domain.Locale(locale)

	return &user, nil
}

func (r *PostgresUserRepository) findOne(ctx context.Context, where string, args ...any) (*domain.User, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM users a WHERE `+where+` LIMIT 1`, args...))
}

// FindByID returns the user with the given id.
func (r *PostgresUserRepository) FindByID(ctx context.Context, id string) (*domain.User, error) {
	return r.findOne(ctx, `a.id = $1`, id)
}

// FindByEmail matches the e-mail case-insensitively.
func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, `lower(a.email) = lower($1)`, strings.TrimSpace(email))
}

// FindByPhone matches a phone in either its "0..." or "+84..." form.
func (r *PostgresUserRepository) FindByPhone(ctx context.Context, phone string) (*domain.User, error) {
	return r.findOne(ctx, `a.phone = ANY($1)`, PhoneVariants(phone))
}

// FindByGoogleSubject returns the user linked to a Google user id.
func (r *PostgresUserRepository) FindByGoogleSubject(ctx context.Context, subject string) (*domain.User, error) {
	return r.findOne(ctx, `a.identity_platform_uid = $1`, subject)
}

// FindByIDs returns the users with the given ids (missing ones are
// skipped).
func (r *PostgresUserRepository) FindByIDs(ctx context.Context, ids []string) ([]*domain.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `SELECT `+userColumns+` FROM users a WHERE a.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User

	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	return users, rows.Err()
}

// Create inserts user and fills its id and timestamps.
func (r *PostgresUserRepository) Create(ctx context.Context, user *domain.User) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO users (email, phone, auth_provider, identity_platform_uid, full_name, password_hash, status, email_verified_at, locale)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		user.Email, user.Phone, string(user.AuthProvider), user.GoogleSubject, user.FullName, user.PasswordHash,
		string(user.Status), user.EmailVerifiedAt, string(user.Locale.OrDefault()),
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	return mapUniqueViolation(err)
}

// Update persists the mutable fields of user.
func (r *PostgresUserRepository) Update(ctx context.Context, user *domain.User) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE users
		SET full_name = $2, phone = $3, locale = $4, password_hash = $5, email_verified_at = $6, identity_platform_uid = $7
		WHERE id = $1
		RETURNING updated_at`,
		user.ID, user.FullName, user.Phone, string(user.Locale.OrDefault()), user.PasswordHash, user.EmailVerifiedAt, user.GoogleSubject,
	).Scan(&user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUserNotFound
	}

	return mapUniqueViolation(err)
}

// GrantPlatformAdmins adds the users with the given e-mails to
// platform_admins.
func (r *PostgresUserRepository) GrantPlatformAdmins(ctx context.Context, emails []string) (int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `
		INSERT INTO platform_admins (user_id)
		SELECT id FROM users WHERE lower(email) = ANY($1)
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

// DeleteTestUsers implements usecase.TestUserDeleter. Each user
// is deleted on its own, so one that cannot be removed (say, a test staff
// member who invited a real user: staff_invitations.invited_by does not
// cascade) does not keep the others; its error is returned with the rest.
func (r *PostgresUserRepository) DeleteTestUsers(ctx context.Context, emailDomain string, createdBefore time.Time) (int64, error) {
	if emailDomain == "" {
		return 0, errors.New("delete test users: empty e-mail domain")
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT id FROM users
		WHERE right(lower(email), length($1) + 1) = '@' || $1 AND created_at < $2`,
		strings.ToLower(emailDomain), createdBefore)
	if err != nil {
		return 0, fmt.Errorf("list test users: %w", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("list test users: %w", err)
	}

	var (
		deleted int64
		errs    []error
	)

	for _, id := range ids {
		// Garages, memberships, subscriptions and the rest cascade.
		if _, err := q.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			errs = append(errs, fmt.Errorf("delete test user %s: %w", id, err))

			continue
		}

		deleted++
	}

	return deleted, errors.Join(errs...)
}

// ListTestUserEmails returns the recipients of test users eligible for purge.
func (r *PostgresUserRepository) ListTestUserEmails(ctx context.Context, emailDomain string, createdBefore time.Time) ([]string, error) {
	if emailDomain == "" {
		return nil, errors.New("list test user e-mails: empty e-mail domain")
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT email FROM users
		WHERE right(lower(email), length($1) + 1) = '@' || $1 AND created_at < $2
		ORDER BY email`,
		strings.ToLower(emailDomain), createdBefore)
	if err != nil {
		return nil, fmt.Errorf("list test user e-mails: %w", err)
	}

	emails, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list test user e-mails: %w", err)
	}

	return emails, nil
}
