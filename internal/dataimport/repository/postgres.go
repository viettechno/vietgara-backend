// Package repository implements the Data Import module's persistence: the
// import_runs history that makes a confirmed file idempotent (FR-INV-14).
package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.RunRepository over the shared pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, r.pool)
}

// Lock takes a transaction-scoped advisory lock on the file, so two
// confirmations of the same content run one after the other.
func (r *PostgresRepository) Lock(ctx context.Context, garageID, importType, sha256 string) error {
	_, err := r.q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		garageID+"|"+importType+"|"+strings.ToLower(sha256))

	return err
}

// Find returns the run with the same garage, type and file hash, or
// (nil, nil).
func (r *PostgresRepository) Find(ctx context.Context, garageID, importType, sha256 string) (*usecase.Run, error) {
	var run usecase.Run

	err := r.q(ctx).QueryRow(ctx, `
		SELECT id, garage_id, import_type, file_sha256, file_name, row_count, document_id, created_by
		FROM import_runs
		WHERE garage_id = $1 AND import_type = $2 AND file_sha256 = $3`,
		garageID, importType, strings.ToLower(sha256)).
		Scan(&run.ID, &run.GarageID, &run.Type, &run.FileSHA256, &run.FileName, &run.RowCount, &run.DocumentID, &run.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &run, nil
}

// Create inserts the run. A concurrent confirmation of the same content
// inserts nothing and returns domain.ErrFileAlreadyImported, so the whole
// import rolls back instead of creating a second document.
func (r *PostgresRepository) Create(ctx context.Context, run *usecase.Run) error {
	tag, err := r.q(ctx).Exec(ctx, `
		INSERT INTO import_runs (id, garage_id, import_type, file_sha256, file_name, row_count, document_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (garage_id, import_type, file_sha256) DO NOTHING`,
		run.ID, run.GarageID, run.Type, strings.ToLower(run.FileSHA256), run.FileName, run.RowCount, run.DocumentID, run.CreatedBy)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrFileAlreadyImported
	}

	return nil
}
