// Package repository implements the persistence layer of the Tenant/Garage
// module on top of the shared Postgres pool. Every query runs under the
// Row-Level Security scope set by the caller's transaction (see
// db.TxManager): garage rows are visible in their garage's scope and to the
// accounts that own or work in them.
// See: Database Design Section 2.2, LLD Section 4.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// PostgresGarageRepository implements usecase.GarageRepository.
type PostgresGarageRepository struct {
	pool *platformdb.Pool
}

// NewPostgresGarageRepository returns a garage repository backed by pool.
func NewPostgresGarageRepository(pool *platformdb.Pool) *PostgresGarageRepository {
	return &PostgresGarageRepository{pool: pool}
}

const garageColumns = `g.id, g.account_id, g.name, g.tax_code, g.address, g.phone, g.email, g.logo_url, g.timezone, g.currency,
	g.status, g.operating_hours, g.quote_number_template, g.repair_order_number_template, g.default_quote_approval_mode,
	g.created_by, g.updated_by, g.created_at, g.updated_at`

func scanGarage(row pgx.Row) (*domain.Garage, error) {
	var (
		g            domain.Garage
		status       string
		approvalMode string
		hours        []byte
	)

	err := row.Scan(&g.ID, &g.OwnerAccountID, &g.Name, &g.TaxCode, &g.Address, &g.Phone, &g.Email, &g.LogoURL,
		&g.Timezone, &g.Currency, &status, &hours, &g.QuoteNumberTemplate, &g.RepairOrderNumberTemplate, &approvalMode,
		&g.CreatedBy, &g.UpdatedBy, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrGarageNotFound
		}

		return nil, err
	}

	g.Status = domain.GarageStatus(status)

	g.DefaultQuoteApprovalMode = domain.QuoteApprovalMode(approvalMode)
	if err := json.Unmarshal(hours, &g.OperatingHours); err != nil {
		return nil, err
	}

	return &g, nil
}

// FindByID returns the garage if it is visible in the current scope.
func (r *PostgresGarageRepository) FindByID(ctx context.Context, id string) (*domain.Garage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanGarage(q.QueryRow(ctx, `SELECT `+garageColumns+` FROM garages g WHERE g.id = $1`, id))
}

// ListForAccount returns the garages accountID owns (any status) or is an
// active member of (active garages only).
func (r *PostgresGarageRepository) ListForAccount(ctx context.Context, accountID string) ([]*domain.Garage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT `+garageColumns+` FROM garages g
		WHERE g.account_id = $1
		   OR (g.status = 'active' AND EXISTS (
		       SELECT 1 FROM garage_staff s WHERE s.garage_id = g.id AND s.account_id = $1 AND s.status = 'active'))
		ORDER BY g.created_at`, accountID)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanGarage)
}

// Create inserts g.
func (r *PostgresGarageRepository) Create(ctx context.Context, g *domain.Garage) error {
	hours, err := json.Marshal(g.OperatingHours)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	return q.QueryRow(ctx, `
		INSERT INTO garages (id, account_id, name, tax_code, address, phone, email, logo_url, timezone, currency, status,
			operating_hours, quote_number_template, repair_order_number_template, default_quote_approval_mode,
			created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING created_at, updated_at`,
		g.ID, g.OwnerAccountID, g.Name, g.TaxCode, g.Address, g.Phone, g.Email, g.LogoURL, g.Timezone, g.Currency,
		string(g.Status), hours, g.QuoteNumberTemplate, g.RepairOrderNumberTemplate, string(g.DefaultQuoteApprovalMode),
		g.CreatedBy, g.UpdatedBy,
	).Scan(&g.CreatedAt, &g.UpdatedAt)
}

// Update persists the mutable fields of g.
func (r *PostgresGarageRepository) Update(ctx context.Context, g *domain.Garage) error {
	hours, err := json.Marshal(g.OperatingHours)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	err = q.QueryRow(ctx, `
		UPDATE garages SET name = $2, tax_code = $3, address = $4, phone = $5, email = $6, logo_url = $7,
			timezone = $8, currency = $9, status = $10, operating_hours = $11, quote_number_template = $12,
			repair_order_number_template = $13, default_quote_approval_mode = $14, updated_by = $15
		WHERE id = $1
		RETURNING updated_at`,
		g.ID, g.Name, g.TaxCode, g.Address, g.Phone, g.Email, g.LogoURL, g.Timezone, g.Currency, string(g.Status),
		hours, g.QuoteNumberTemplate, g.RepairOrderNumberTemplate, string(g.DefaultQuoteApprovalMode), g.UpdatedBy,
	).Scan(&g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrGarageNotFound
	}

	return err
}

// collect scans every row with scan and closes rows.
func collect[T any](rows pgx.Rows, scan func(pgx.Row) (T, error)) ([]T, error) {
	defer rows.Close()

	var out []T

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, item)
	}

	return out, rows.Err()
}

// isUniqueViolation reports a unique-constraint error, optionally on a
// constraint whose name contains fragment.
func isUniqueViolation(err error, fragment string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (fragment == "" || strings.Contains(pgErr.ConstraintName, fragment))
}

// isForeignKeyViolation reports a foreign-key error.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
