// Package repository implements the ports of the Document/Print use case on
// PostgreSQL.
package repository

import (
	"context"
	"fmt"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresPrintStore implements usecase.PrintStore on the shared pool. Both
// tables are garage-scoped by row-level security: every call must run in the
// garage's transaction, which the Garage interceptor opens for each request.
type PostgresPrintStore struct {
	pool *platformdb.Pool
}

// NewPostgresPrintStore returns a PostgresPrintStore on pool.
func NewPostgresPrintStore(pool *platformdb.Pool) *PostgresPrintStore {
	return &PostgresPrintStore{pool: pool}
}

// Settings returns the settings the garage has saved.
func (s *PostgresPrintStore) Settings(ctx context.Context, garageID string) (map[printmodel.DocumentType]usecase.PrintSetting, error) {
	rows, err := platformdb.QuerierFromContext(ctx, s.pool).Query(ctx,
		`SELECT document_type, paper_size, copy_mark FROM garage_print_settings WHERE garage_id = $1`, garageID)
	if err != nil {
		return nil, fmt.Errorf("query print settings: %w", err)
	}
	defer rows.Close()

	settings := map[printmodel.DocumentType]usecase.PrintSetting{}

	for rows.Next() {
		var (
			documentType, paper string
			setting             usecase.PrintSetting
		)

		if err := rows.Scan(&documentType, &paper, &setting.CopyMark); err != nil {
			return nil, fmt.Errorf("scan print setting: %w", err)
		}

		setting.Paper = printmodel.Paper(paper)
		settings[printmodel.DocumentType(documentType)] = setting
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read print settings: %w", err)
	}

	return settings, nil
}

// SaveSetting inserts or replaces the setting of one document type.
func (s *PostgresPrintStore) SaveSetting(ctx context.Context, garageID string, t printmodel.DocumentType, setting usecase.PrintSetting) error {
	_, err := platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx, `
		INSERT INTO garage_print_settings (garage_id, document_type, paper_size, copy_mark)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (garage_id, document_type)
		DO UPDATE SET paper_size = EXCLUDED.paper_size, copy_mark = EXCLUDED.copy_mark`,
		garageID, string(t), string(setting.Paper), setting.CopyMark)
	if err != nil {
		return fmt.Errorf("save print setting: %w", err)
	}

	return nil
}

// State reads what the print log says about a document: the template
// version of its first print on paper, and how many times it was printed.
func (s *PostgresPrintStore) State(
	ctx context.Context, garageID string, t printmodel.DocumentType, documentID string, paper printmodel.Paper,
) (usecase.PrintState, error) {
	var state usecase.PrintState

	err := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx, `
		SELECT COALESCE((SELECT template_version FROM print_log
		                 WHERE garage_id = $1 AND document_type = $2 AND document_id = $3 AND paper_size = $4
		                 ORDER BY created_at, id LIMIT 1), 0),
		       (SELECT count(*) FROM print_log WHERE garage_id = $1 AND document_type = $2 AND document_id = $3)`,
		garageID, string(t), documentID, string(paper)).Scan(&state.FirstVersion, &state.Prints)
	if err != nil {
		return usecase.PrintState{}, fmt.Errorf("read print state: %w", err)
	}

	return state, nil
}

// Record adds one print to the log. The user and the time come from the
// audit trigger.
func (s *PostgresPrintStore) Record(
	ctx context.Context, garageID string, t printmodel.DocumentType, documentID string, paper printmodel.Paper, version int, copyMark bool,
) error {
	_, err := platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx, `
		INSERT INTO print_log (garage_id, document_type, document_id, paper_size, template_version, copy)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		garageID, string(t), documentID, string(paper), version, copyMark)
	if err != nil {
		return fmt.Errorf("log print: %w", err)
	}

	return nil
}
