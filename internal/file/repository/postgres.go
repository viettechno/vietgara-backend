// Package repository implements the file registry on the shared Postgres
// pool. Every query filters on garage_id explicitly on top of Row-Level
// Security and skips soft-deleted rows. See: Database Design (files).
package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const fileColumns = `id, garage_id, kind, entity_type, entity_id, key, content_type, size, sha256, is_active, created_by, created_at`

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a registry backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func scanFile(row pgx.Row) (*domain.File, error) {
	var (
		f    domain.File
		kind string
	)

	err := row.Scan(&f.ID, &f.GarageID, &kind, &f.EntityType, &f.EntityID, &f.Key, &f.ContentType, &f.Size, &f.SHA256,
		&f.IsActive, &f.CreatedBy, &f.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrFileNotFound
		}

		return nil, err
	}

	f.Kind = domain.Kind(kind)

	return &f, nil
}

// Create inserts f and scans back the server-generated timestamp.
func (r *PostgresRepository) Create(ctx context.Context, f *domain.File) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return q.QueryRow(ctx, `
		INSERT INTO files (id, garage_id, kind, entity_type, entity_id, key, content_type, size, sha256, is_active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at`,
		f.ID, f.GarageID, string(f.Kind), f.EntityType, f.EntityID, f.Key, f.ContentType, f.Size, f.SHA256, f.IsActive, f.CreatedBy,
	).Scan(&f.CreatedAt)
}

// FindByID returns the live file, or domain.ErrFileNotFound.
func (r *PostgresRepository) FindByID(ctx context.Context, garageID, fileID string) (*domain.File, error) {
	if uuid.Validate(fileID) != nil {
		return nil, domain.ErrFileNotFound
	}

	return scanFile(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+fileColumns+` FROM files WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`, garageID, fileID))
}

// FindByKey returns the live file with the object key, or
// domain.ErrFileNotFound.
func (r *PostgresRepository) FindByKey(ctx context.Context, garageID, key string) (*domain.File, error) {
	return scanFile(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+fileColumns+` FROM files WHERE garage_id = $1 AND key = $2 AND deleted_at IS NULL`, garageID, key))
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func (r *PostgresRepository) exec(ctx context.Context, query string, args ...any) error {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrFileNotFound
	}

	return nil
}

// SoftDelete marks the file deleted, or returns domain.ErrFileNotFound.
func (r *PostgresRepository) SoftDelete(ctx context.Context, garageID, fileID, deletedBy string) error {
	if uuid.Validate(fileID) != nil {
		return domain.ErrFileNotFound
	}

	return r.exec(ctx, `
		UPDATE files SET deleted_at = now(), updated_by = $3
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`, garageID, fileID, nullable(deletedBy))
}

// SetActive deactivates or reactivates the file, or returns
// domain.ErrFileNotFound.
func (r *PostgresRepository) SetActive(ctx context.Context, garageID, fileID string, active bool, updatedBy string) error {
	if uuid.Validate(fileID) != nil {
		return domain.ErrFileNotFound
	}

	return r.exec(ctx, `
		UPDATE files SET is_active = $3, updated_by = $4
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`, garageID, fileID, active, nullable(updatedBy))
}
