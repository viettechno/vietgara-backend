// Package repository implements the persistence layer of the Tenant/Garage
// module on top of the shared Postgres pool. Every query runs under the
// Row-Level Security scope set by the caller's transaction (see
// db.TxManager): garage rows are visible in their garage's scope and to the
// users that own or work in them.
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

const garageColumns = `g.id, g.user_id, g.name, g.tax_code, g.legal_name, g.legal_address, g.representative, g.bank_details, g.address, g.phone,
	g.email, g.logo_key, g.timezone, g.currency, g.status, g.operating_hours, g.quote_number_template,
	g.repair_order_number_template, g.settlement_number_template, g.payment_voucher_number_template,
	g.default_quote_approval_mode, g.created_by, g.updated_by, g.created_at, g.updated_at`

func scanGarage(row pgx.Row) (*domain.Garage, error) {
	var (
		g            domain.Garage
		status       string
		approvalMode string
		hours        []byte
		bank         []byte
	)

	err := row.Scan(&g.ID, &g.OwnerUserID, &g.Name, &g.TaxCode, &g.LegalName, &g.LegalAddress, &g.Representative, &bank,
		&g.Address, &g.Phone, &g.Email, &g.LogoKey, &g.Timezone, &g.Currency, &status, &hours, &g.QuoteNumberTemplate,
		&g.RepairOrderNumberTemplate, &g.SettlementNumberTemplate, &g.PaymentVoucherNumberTemplate, &approvalMode,
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

	if len(bank) > 0 {
		if err := json.Unmarshal(bank, &g.Bank); err != nil {
			return nil, err
		}
	}

	return &g, nil
}

// FindByID returns the garage if it is visible in the current scope.
func (r *PostgresGarageRepository) FindByID(ctx context.Context, id string) (*domain.Garage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanGarage(q.QueryRow(ctx, `SELECT `+garageColumns+` FROM garages g WHERE g.id = $1`, id))
}

// ListForUser returns the garages userID owns (any status) or is an
// active member of (active garages only).
func (r *PostgresGarageRepository) ListForUser(ctx context.Context, userID string) ([]*domain.Garage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT `+garageColumns+` FROM garages g
		WHERE g.user_id = $1
		   OR (g.status = 'active' AND EXISTS (
		       SELECT 1 FROM garage_staff s WHERE s.garage_id = g.id AND s.user_id = $1 AND s.status = 'active'))
		ORDER BY g.created_at`, userID)
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

	bank, err := json.Marshal(g.Bank)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	return q.QueryRow(ctx, `
		INSERT INTO garages (id, user_id, name, tax_code, legal_name, legal_address, representative, bank_details, address,
			phone, email, logo_key, timezone, currency, status, operating_hours, quote_number_template,
			repair_order_number_template, settlement_number_template, payment_voucher_number_template,
			default_quote_approval_mode, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		RETURNING created_at, updated_at`,
		g.ID, g.OwnerUserID, g.Name, g.TaxCode, g.LegalName, g.LegalAddress, g.Representative, bank, g.Address, g.Phone,
		g.Email, g.LogoKey, g.Timezone, g.Currency, string(g.Status), hours, g.QuoteNumberTemplate,
		g.RepairOrderNumberTemplate, g.SettlementNumberTemplate, g.PaymentVoucherNumberTemplate,
		string(g.DefaultQuoteApprovalMode), g.CreatedBy, g.UpdatedBy,
	).Scan(&g.CreatedAt, &g.UpdatedAt)
}

// Update persists the mutable fields of g.
func (r *PostgresGarageRepository) Update(ctx context.Context, g *domain.Garage) error {
	hours, err := json.Marshal(g.OperatingHours)
	if err != nil {
		return err
	}

	bank, err := json.Marshal(g.Bank)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	err = q.QueryRow(ctx, `
		UPDATE garages SET name = $2, tax_code = $3, legal_name = $4, legal_address = $5, representative = $6,
			bank_details = $7, address = $8, phone = $9, email = $10, logo_key = $11, timezone = $12, currency = $13,
			status = $14, operating_hours = $15, quote_number_template = $16, repair_order_number_template = $17,
			settlement_number_template = $18, payment_voucher_number_template = $19, default_quote_approval_mode = $20,
			updated_by = $21
		WHERE id = $1
		RETURNING updated_at`,
		g.ID, g.Name, g.TaxCode, g.LegalName, g.LegalAddress, g.Representative, bank, g.Address, g.Phone, g.Email,
		g.LogoKey, g.Timezone, g.Currency, string(g.Status), hours, g.QuoteNumberTemplate, g.RepairOrderNumberTemplate,
		g.SettlementNumberTemplate, g.PaymentVoucherNumberTemplate, string(g.DefaultQuoteApprovalMode), g.UpdatedBy,
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
