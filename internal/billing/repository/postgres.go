// Package repository implements the persistence layer of the Billing &
// Settlement module on top of the shared Postgres pool. Every query
// filters on garage_id explicitly on top of Row-Level Security; it never
// queries another module's tables (customer name and vehicle plate are
// denormalized on the settlement itself, see migration 000018). See: Database Design Section 6.1.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const pgUniqueViolation = "23505"

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a settlement repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const settlementColumns = `id, garage_id, repair_order_id, vehicle_license_plate, customer_name, number, total_amount,
	paid_amount, status, created_by, updated_by, created_at, updated_at`

func scanSettlement(row pgx.Row) (*domain.Settlement, error) {
	var (
		s      domain.Settlement
		status string
	)

	err := row.Scan(&s.ID, &s.GarageID, &s.RepairOrderID, &s.VehicleLicensePlate, &s.CustomerName, &s.Number,
		&s.TotalAmount, &s.PaidAmount, &status, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSettlementNotFound
		}

		return nil, err
	}

	s.Status = domain.SettlementStatus(status)

	return &s, nil
}

const paymentColumns = `id, amount, method, note, created_by, created_at`

func scanPayment(row pgx.Row) (domain.Payment, error) {
	var (
		p      domain.Payment
		method string
		note   *string
	)

	err := row.Scan(&p.ID, &p.Amount, &method, &note, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return domain.Payment{}, err
	}

	p.Method = domain.PaymentMethod(method)
	if note != nil {
		p.Note = *note
	}

	return p, nil
}

// Create inserts s. A repair order already settled returns
// domain.ErrAlreadySettled.
func (r *PostgresRepository) Create(ctx context.Context, s *domain.Settlement) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		INSERT INTO settlements (id, garage_id, repair_order_id, vehicle_license_plate, customer_name, number,
			total_amount, status, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`,
		s.ID, s.GarageID, s.RepairOrderID, s.VehicleLicensePlate, s.CustomerName, s.Number, s.TotalAmount,
		string(s.Status), s.CreatedBy, s.UpdatedBy,
	)

	err := row.Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "settlements_repair_order_id_unique") {
			return domain.ErrAlreadySettled
		}

		return err
	}

	return nil
}

// FindByID returns the settlement with its payments, or
// domain.ErrSettlementNotFound.
func (r *PostgresRepository) FindByID(ctx context.Context, garageID, settlementID string) (*domain.Settlement, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	s, err := scanSettlement(q.QueryRow(ctx, `
		SELECT `+settlementColumns+` FROM settlements WHERE garage_id = $1 AND id = $2`, garageID, settlementID))
	if err != nil {
		return nil, err
	}

	payments, err := r.paymentsFor(ctx, q, garageID, settlementID)
	if err != nil {
		return nil, err
	}

	s.Payments = payments

	return s, nil
}

func (r *PostgresRepository) paymentsFor(ctx context.Context, q platformdb.Querier, garageID, settlementID string) ([]domain.Payment, error) {
	rows, err := q.Query(ctx, `
		SELECT `+paymentColumns+` FROM settlement_payments WHERE garage_id = $1 AND settlement_id = $2 ORDER BY created_at`,
		garageID, settlementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []domain.Payment

	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}

		payments = append(payments, p)
	}

	return payments, rows.Err()
}

// FindByRepairOrderID returns the repair order's settlement, if any, or
// domain.ErrSettlementNotFound.
func (r *PostgresRepository) FindByRepairOrderID(ctx context.Context, garageID, repairOrderID string) (*domain.Settlement, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanSettlement(q.QueryRow(ctx, `
		SELECT `+settlementColumns+` FROM settlements WHERE garage_id = $1 AND repair_order_id = $2`,
		garageID, repairOrderID))
}

// SaveStatus persists a settlement's status alone, after RecordPayment.
func (r *PostgresRepository) SaveStatus(ctx context.Context, garageID, settlementID string, status domain.SettlementStatus) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `UPDATE settlements SET status = $3 WHERE garage_id = $1 AND id = $2`,
		garageID, settlementID, string(status))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrSettlementNotFound
	}

	return nil
}

// List returns one page of live settlements (without payments), newest
// first.
func (r *PostgresRepository) List(ctx context.Context, garageID string, filter usecase.ListFilter) ([]*domain.Settlement, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := `WHERE garage_id = $1
		AND ($2::settlement_status IS NULL OR status = $2::settlement_status)
		AND (NOT $3 OR status IN ('pending', 'partially_paid'))`
	args := []any{garageID, nullableString(string(filter.Status)), filter.OutstandingOnly}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM settlements `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+settlementColumns+` FROM settlements `+where+`
		ORDER BY created_at DESC, id LIMIT $4 OFFSET $5`,
		append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.Settlement, 0, filter.Limit)

	for rows.Next() {
		item, err := scanSettlement(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// RecordPayment inserts payment and atomically applies it to the
// settlement's running paid total, returning the settlement's fresh
// amounts (its Payments field is left empty; the caller re-fetches if it
// needs the full history).
func (r *PostgresRepository) RecordPayment(
	ctx context.Context, garageID, settlementID string, payment *domain.Payment,
) (*domain.Settlement, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		INSERT INTO settlement_payments (id, garage_id, settlement_id, amount, method, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		payment.ID, garageID, settlementID, payment.Amount, string(payment.Method),
		nullableString(payment.Note), payment.CreatedBy,
	)
	if err := row.Scan(&payment.CreatedAt); err != nil {
		return nil, err
	}

	s, err := scanSettlement(q.QueryRow(ctx, `
		UPDATE settlements SET paid_amount = paid_amount + $3
		WHERE garage_id = $1 AND id = $2
		RETURNING `+settlementColumns,
		garageID, settlementID, payment.Amount,
	))
	if err != nil {
		return nil, err
	}

	return s, nil
}

// RevenueBucket is one period's total settlement value.
type RevenueBucket struct {
	PeriodStart time.Time
	Amount      int64
}

// truncUnits are the only values ever interpolated into date_trunc below;
// RevenueByPeriod rejects anything else before it reaches SQL.
var truncUnits = map[string]bool{"day": true, "month": true, "quarter": true, "year": true}

// RevenueByPeriod sums settlements.total_amount per period, bucketed by
// the settlement's created_at truncated to unit ("day", "month",
// "quarter" or "year") -- FR-RPT-01. Called directly by an adapter in
// cmd/server for the Reporting module (HLD module #15), the same choice
// documentQuotes already makes for a single cross-module read with no
// business rule attached, rather than going through this module's own
// use case. from is inclusive, to is exclusive.
func (r *PostgresRepository) RevenueByPeriod(ctx context.Context, garageID, unit string, from, to time.Time) ([]RevenueBucket, error) {
	if !truncUnits[unit] {
		return nil, fmt.Errorf("billing: invalid period unit %q", unit)
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT date_trunc($2, created_at) AS period, COALESCE(SUM(total_amount), 0)
		FROM settlements
		WHERE garage_id = $1 AND created_at >= $3 AND created_at < $4
		GROUP BY period
		ORDER BY period`,
		garageID, unit, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []RevenueBucket

	for rows.Next() {
		var b RevenueBucket
		if err := rows.Scan(&b.PeriodStart, &b.Amount); err != nil {
			return nil, err
		}

		buckets = append(buckets, b)
	}

	return buckets, rows.Err()
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == constraint
}
