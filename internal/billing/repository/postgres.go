// Package repository implements the persistence layer of the Billing &
// Settlement module on top of the shared Postgres pool. Every query
// filters on garage_id explicitly on top of Row-Level Security; it never
// queries another module's tables (customer name and vehicle plate are
// denormalized on the settlement itself, see the settlements migration). See: Database Design Section 6.1.
package repository

import (
	"context"
	"encoding/json"
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

var errInvalidRevenuePeriodUnit = errors.New("billing: invalid period unit")

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
	paid_amount, issuer, billing_party, invoice_requested, status, created_by, updated_by, created_at, updated_at`

func scanSettlement(row pgx.Row) (*domain.Settlement, error) {
	var (
		s      domain.Settlement
		status string
		issuer []byte
		party  []byte
	)

	err := row.Scan(&s.ID, &s.GarageID, &s.RepairOrderID, &s.VehicleLicensePlate, &s.CustomerName, &s.Number,
		&s.TotalAmount, &s.PaidAmount, &issuer, &party, &s.InvoiceRequested, &status, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSettlementNotFound
		}

		return nil, err
	}

	s.Status = domain.SettlementStatus(status)

	if len(issuer) > 0 {
		if err := json.Unmarshal(issuer, &s.Issuer); err != nil {
			return nil, err
		}
	}

	if len(party) > 0 {
		if err := json.Unmarshal(party, &s.BillingParty); err != nil {
			return nil, err
		}
	}

	return &s, nil
}

const paymentColumns = `id, amount, method, note, voucher_number, payer_name, payer_address, reason, collector_name, issuer,
	void_reason, voided_at, created_by, created_at`

func scanPayment(row pgx.Row) (domain.Payment, error) {
	var (
		p            domain.Payment
		method       string
		note         *string
		payerAddress *string
		voidReason   *string
		issuer       []byte
	)

	err := row.Scan(&p.ID, &p.Amount, &method, &note, &p.VoucherNumber, &p.PayerName, &payerAddress, &p.Reason,
		&p.CollectorName, &issuer, &voidReason, &p.VoidedAt, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return domain.Payment{}, err
	}

	if payerAddress != nil {
		p.PayerAddress = *payerAddress
	}

	if voidReason != nil {
		p.VoidReason = *voidReason
	}

	if len(issuer) > 0 {
		if err := json.Unmarshal(issuer, &p.Issuer); err != nil {
			return domain.Payment{}, err
		}
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

	issuer, err := json.Marshal(s.Issuer)
	if err != nil {
		return err
	}

	party, err := json.Marshal(s.BillingParty)
	if err != nil {
		return err
	}

	row := q.QueryRow(ctx, `
		INSERT INTO settlements (id, garage_id, repair_order_id, vehicle_license_plate, customer_name, number,
			total_amount, issuer, billing_party, invoice_requested, status, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at`,
		s.ID, s.GarageID, s.RepairOrderID, s.VehicleLicensePlate, s.CustomerName, s.Number, s.TotalAmount,
		issuer, party, s.InvoiceRequested, string(s.Status), s.CreatedBy, s.UpdatedBy,
	)

	err = row.Scan(&s.CreatedAt, &s.UpdatedAt)
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

// SaveBilling persists who the settlement is billed to and whether an
// invoice was requested.
func (r *PostgresRepository) SaveBilling(ctx context.Context, s *domain.Settlement) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	party, err := json.Marshal(s.BillingParty)
	if err != nil {
		return err
	}

	tag, err := q.Exec(ctx, `
		UPDATE settlements SET billing_party = $3, invoice_requested = $4, updated_by = $5
		WHERE garage_id = $1 AND id = $2`,
		s.GarageID, s.ID, party, s.InvoiceRequested, s.UpdatedBy)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrSettlementNotFound
	}

	return nil
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

	issuer, err := json.Marshal(payment.Issuer)
	if err != nil {
		return nil, err
	}

	row := q.QueryRow(ctx, `
		INSERT INTO settlement_payments (id, garage_id, settlement_id, amount, method, note, voucher_number, payer_name,
			payer_address, reason, collector_name, issuer, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at`,
		payment.ID, garageID, settlementID, payment.Amount, string(payment.Method), nullableString(payment.Note),
		payment.VoucherNumber, payment.PayerName, nullableString(payment.PayerAddress), payment.Reason, payment.CollectorName,
		issuer, payment.CreatedBy,
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

// VoidPayment marks the payment void and takes its amount off the
// settlement's paid total, returning the settlement's fresh amounts (its
// Payments field is left empty).
func (r *PostgresRepository) VoidPayment(
	ctx context.Context, garageID, settlementID, paymentID, reason string,
) (*domain.Settlement, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var amount int64

	err := q.QueryRow(ctx, `
		UPDATE settlement_payments SET voided_at = NOW(), void_reason = $4
		WHERE garage_id = $1 AND settlement_id = $2 AND id = $3 AND voided_at IS NULL
		RETURNING amount`, garageID, settlementID, paymentID, reason).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, r.whyNotVoided(ctx, q, garageID, settlementID, paymentID)
	}

	if err != nil {
		return nil, err
	}

	return scanSettlement(q.QueryRow(ctx, `
		UPDATE settlements SET paid_amount = paid_amount - $3
		WHERE garage_id = $1 AND id = $2
		RETURNING `+settlementColumns, garageID, settlementID, amount))
}

// whyNotVoided tells a payment that does not exist from one already void.
func (r *PostgresRepository) whyNotVoided(ctx context.Context, q platformdb.Querier, garageID, settlementID, paymentID string) error {
	var exists bool

	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM settlement_payments WHERE garage_id = $1 AND settlement_id = $2 AND id = $3)`,
		garageID, settlementID, paymentID).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		return domain.ErrPaymentNotFound
	}

	return domain.ErrPaymentAlreadyVoided
}

// BilledToTaxProfile reports whether a settlement was billed to the tax
// profile, which keeps the profile from being deleted instead of
// deactivated (FR-CAT-07).
func (r *PostgresRepository) BilledToTaxProfile(ctx context.Context, garageID, profileID string) (bool, error) {
	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM settlements WHERE garage_id = $1 AND billing_party->>'tax_profile_id' = $2)`,
		garageID, profileID).Scan(&exists)

	return exists, err
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
// the settlement's created_at in timezone (the garage's IANA zone, so a
// day is the garage's day) truncated to unit ("day", "month", "quarter"
// or "year") -- FR-RPT-01. from (inclusive) and to (exclusive) are
// calendar dates of timezone (only their year, month and day are read):
// Postgres turns them into instants with the same zone rules it buckets
// with. A bucket's PeriodStart is that local start as a wall-clock time
// (its location is not meaningful). Called directly by an adapter in
// cmd/server for the Reporting module (HLD module #15), the same choice
// documentQuotes already makes for a single cross-module read with no
// business rule attached, rather than going through this module's own
// use case. from is inclusive, to is exclusive.
func (r *PostgresRepository) RevenueByPeriod(ctx context.Context, garageID, unit, timezone string, from, to time.Time) ([]RevenueBucket, error) {
	if !truncUnits[unit] {
		return nil, fmt.Errorf("%w %q", errInvalidRevenuePeriodUnit, unit)
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT date_trunc($2, created_at AT TIME ZONE $5) AS period, COALESCE(SUM(total_amount), 0)
		FROM settlements
		WHERE garage_id = $1
			AND created_at >= $3::date::timestamp AT TIME ZONE $5
			AND created_at < $4::date::timestamp AT TIME ZONE $5
		GROUP BY period
		ORDER BY period`,
		garageID, unit, from.Format(time.DateOnly), to.Format(time.DateOnly), timezone,
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
