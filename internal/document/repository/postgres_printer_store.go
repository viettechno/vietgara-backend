package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresPrinterStore implements usecase.PrinterStore on the shared pool.
// garage_printers is garage-scoped by row-level security: every call must run
// in the garage's transaction.
type PostgresPrinterStore struct {
	pool *platformdb.Pool
}

// NewPostgresPrinterStore returns a PostgresPrinterStore on pool.
func NewPostgresPrinterStore(pool *platformdb.Pool) *PostgresPrinterStore {
	return &PostgresPrinterStore{pool: pool}
}

const printerColumns = `id, name, protocol, host, port, path, COALESCE(paper_size, ''), document_types, created_at, updated_at`

// ListPrinters returns the garage's printers by name.
func (s *PostgresPrinterStore) ListPrinters(ctx context.Context, garageID string) ([]usecase.Printer, error) {
	rows, err := platformdb.QuerierFromContext(ctx, s.pool).Query(ctx,
		`SELECT `+printerColumns+` FROM garage_printers WHERE garage_id = $1 ORDER BY lower(name), id`, garageID)
	if err != nil {
		return nil, fmt.Errorf("query printers: %w", err)
	}
	defer rows.Close()

	printers := []usecase.Printer{}

	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}

		printers = append(printers, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read printers: %w", err)
	}

	return printers, nil
}

// GetPrinter returns one printer.
func (s *PostgresPrinterStore) GetPrinter(ctx context.Context, garageID, id string) (usecase.Printer, error) {
	if uuid.Validate(id) != nil {
		return usecase.Printer{}, usecase.ErrPrinterNotFound
	}

	row := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx,
		`SELECT `+printerColumns+` FROM garage_printers WHERE garage_id = $1 AND id = $2`, garageID, id)

	return printerResult(scanPrinter(row))
}

// LockPrinters takes a transaction-scoped advisory lock on the garage's
// printers.
func (s *PostgresPrinterStore) LockPrinters(ctx context.Context, garageID string) error {
	_, err := platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('garage_printers:' || $1, 0))`, garageID)
	if err != nil {
		return fmt.Errorf("lock printers: %w", err)
	}

	return nil
}

// CountPrinters counts the garage's printers.
func (s *PostgresPrinterStore) CountPrinters(ctx context.Context, garageID string) (int, error) {
	var n int
	if err := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx,
		`SELECT count(*) FROM garage_printers WHERE garage_id = $1`, garageID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count printers: %w", err)
	}

	return n, nil
}

// CreatePrinter inserts a printer.
func (s *PostgresPrinterStore) CreatePrinter(ctx context.Context, garageID string, p usecase.Printer) (usecase.Printer, error) {
	row := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx, `
		INSERT INTO garage_printers (garage_id, name, protocol, host, port, path, paper_size, document_types)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)
		RETURNING `+printerColumns,
		garageID, p.Name, string(p.Protocol), p.Host, p.Port, p.Path, string(p.Paper), typeStrings(p.DocumentTypes))

	return printerResult(scanPrinter(row))
}

// UpdatePrinter replaces a printer.
func (s *PostgresPrinterStore) UpdatePrinter(ctx context.Context, garageID string, p usecase.Printer) (usecase.Printer, error) {
	if uuid.Validate(p.ID) != nil {
		return usecase.Printer{}, usecase.ErrPrinterNotFound
	}

	row := platformdb.QuerierFromContext(ctx, s.pool).QueryRow(ctx, `
		UPDATE garage_printers
		SET name = $3, protocol = $4, host = $5, port = $6, path = $7, paper_size = NULLIF($8, ''), document_types = $9
		WHERE garage_id = $1 AND id = $2
		RETURNING `+printerColumns,
		garageID, p.ID, p.Name, string(p.Protocol), p.Host, p.Port, p.Path, string(p.Paper), typeStrings(p.DocumentTypes))

	return printerResult(scanPrinter(row))
}

// DeletePrinter removes a printer.
func (s *PostgresPrinterStore) DeletePrinter(ctx context.Context, garageID, id string) error {
	if uuid.Validate(id) != nil {
		return usecase.ErrPrinterNotFound
	}

	tag, err := platformdb.QuerierFromContext(ctx, s.pool).Exec(ctx,
		`DELETE FROM garage_printers WHERE garage_id = $1 AND id = $2`, garageID, id)
	if err != nil {
		return fmt.Errorf("delete printer: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return usecase.ErrPrinterNotFound
	}

	return nil
}

func scanPrinter(row pgx.Row) (usecase.Printer, error) {
	var (
		p               usecase.Printer
		protocol, paper string
		documentTypes   []string
	)

	if err := row.Scan(&p.ID, &p.Name, &protocol, &p.Host, &p.Port, &p.Path, &paper, &documentTypes, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return usecase.Printer{}, err //nolint:wrapcheck // printerResult classifies it
	}

	p.Protocol, p.Paper = usecase.PrinterProtocol(protocol), printmodel.Paper(paper)

	p.DocumentTypes = make([]printmodel.DocumentType, 0, len(documentTypes))
	for _, t := range documentTypes {
		p.DocumentTypes = append(p.DocumentTypes, printmodel.DocumentType(t))
	}

	return p, nil
}

// printerResult maps a missing row and a duplicate name to the store's
// errors. A malformed id never reaches the database: it would abort the
// transaction.
func printerResult(p usecase.Printer, err error) (usecase.Printer, error) {
	var pgErr *pgconn.PgError

	switch {
	case err == nil:
		return p, nil
	case errors.Is(err, pgx.ErrNoRows):
		return usecase.Printer{}, usecase.ErrPrinterNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return usecase.Printer{}, usecase.ErrPrinterNameTaken
	default:
		return usecase.Printer{}, fmt.Errorf("printer: %w", err)
	}
}

func typeStrings(types []printmodel.DocumentType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}

	return out
}
