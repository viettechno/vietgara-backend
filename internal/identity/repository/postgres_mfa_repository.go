package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresMFARepository implements usecase.MFARepository on the shared
// pool. Like users, the MFA tables sit above the tenant level and have no
// row-level security.
type PostgresMFARepository struct {
	pool *platformdb.Pool
}

// NewPostgresMFARepository returns a PostgresMFARepository on pool.
func NewPostgresMFARepository(pool *platformdb.Pool) *PostgresMFARepository {
	return &PostgresMFARepository{pool: pool}
}

// Find returns the user's enrolment, pending or enabled, or
// domain.ErrMFANotFound.
func (r *PostgresMFARepository) Find(ctx context.Context, userID string) (*domain.MFA, error) {
	m := domain.MFA{UserID: userID}

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT secret_encrypted, enabled_at, last_used_step FROM user_mfa WHERE user_id = $1`, userID,
	).Scan(&m.SecretEncrypted, &m.EnabledAt, &m.LastUsedStep)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMFANotFound
	}

	if err != nil {
		return nil, err
	}

	return &m, nil
}

// SavePending stores the secret of a new enrolment, replacing an earlier
// pending one. It does nothing to an enabled factor and reports false then.
func (r *PostgresMFARepository) SavePending(ctx context.Context, userID string, secretEncrypted []byte) (bool, error) {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		INSERT INTO user_mfa (user_id, secret_encrypted) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET secret_encrypted = EXCLUDED.secret_encrypted, last_used_step = 0
		WHERE user_mfa.enabled_at IS NULL`, userID, secretEncrypted)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

// Enable confirms the pending enrolment at enabledAt, records the step of
// the confirming code and stores the recovery code hashes.
func (r *PostgresMFARepository) Enable(ctx context.Context, userID string, enabledAt time.Time, step int64, codeHashes []string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx,
		`UPDATE user_mfa SET enabled_at = $2, last_used_step = $3 WHERE user_id = $1 AND enabled_at IS NULL`,
		userID, enabledAt, step)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrMFAEnrollmentNotStarted
	}

	return r.ReplaceRecoveryCodes(ctx, userID, codeHashes)
}

// ReplaceRecoveryCodes deletes the user's recovery codes and stores the new
// hashes.
func (r *PostgresMFARepository) ReplaceRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if _, err := q.Exec(ctx, `DELETE FROM user_mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}

	_, err := q.Exec(ctx,
		`INSERT INTO user_mfa_recovery_codes (user_id, code_hash) SELECT $1, unnest($2::text[])`, userID, codeHashes)

	return err
}

// AdvanceStep records step as used. It reports false when step is not newer
// than the last accepted one: the code was already used.
func (r *PostgresMFARepository) AdvanceStep(ctx context.Context, userID string, step int64) (bool, error) {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx,
		`UPDATE user_mfa SET last_used_step = $2 WHERE user_id = $1 AND last_used_step < $2`, userID, step)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

// UseRecoveryCode spends the recovery code with this hash; it reports false
// when there is none or it was already spent.
func (r *PostgresMFARepository) UseRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx,
		`UPDATE user_mfa_recovery_codes SET used_at = NOW() WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`,
		userID, codeHash)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

// RecoveryCodesRemaining counts the unspent recovery codes.
func (r *PostgresMFARepository) RecoveryCodesRemaining(ctx context.Context, userID string) (int, error) {
	var n int

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM user_mfa_recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&n)

	return n, err
}

// Delete removes the enrolment and its recovery codes.
func (r *PostgresMFARepository) Delete(ctx context.Context, userID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if _, err := q.Exec(ctx, `DELETE FROM user_mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}

	_, err := q.Exec(ctx, `DELETE FROM user_mfa WHERE user_id = $1`, userID)

	return err
}
