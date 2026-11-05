package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

const payableColumns = `id, garage_id, goods_receipt_id, goods_receipt_number, supplier_id, supplier_name, receipt_date,
	total_amount, paid_amount, status, created_at`

func scanPayable(row pgx.Row) (*domain.Payable, error) {
	var (
		p      domain.Payable
		status string
	)

	err := row.Scan(&p.ID, &p.GarageID, &p.GoodsReceiptID, &p.GoodsReceiptNumber, &p.SupplierID, &p.SupplierName, &p.ReceiptDate,
		&p.TotalAmount, &p.PaidAmount, &status, &p.CreatedAt)
	if err != nil {
		return nil, err
	}

	p.Status = domain.PayableStatus(status)

	return &p, nil
}

// AddPayable inserts the payable unless the receipt already made one for the
// supplier.
func (r *PostgresRepository) AddPayable(ctx context.Context, p *domain.Payable) error {
	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO supplier_payables (garage_id, goods_receipt_id, goods_receipt_number, supplier_id, supplier_name,
			receipt_date, total_amount)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7)
		ON CONFLICT (garage_id, goods_receipt_id, supplier_id) DO NOTHING`,
		p.GarageID, p.GoodsReceiptID, p.GoodsReceiptNumber, p.SupplierID, p.SupplierName, p.ReceiptDate, p.TotalAmount)
	if err != nil {
		return fmt.Errorf("add supplier payable: %w", err)
	}

	return nil
}

// CancelPayables marks the payables of a goods receipt cancelled.
func (r *PostgresRepository) CancelPayables(ctx context.Context, garageID, goodsReceiptID string) error {
	_, err := r.q(ctx).Exec(ctx, `
		UPDATE supplier_payables SET status = 'cancelled'
		WHERE garage_id = $1 AND goods_receipt_id = $2 AND status <> 'cancelled'`, garageID, goodsReceiptID)
	if err != nil {
		return fmt.Errorf("cancel supplier payables: %w", err)
	}

	return nil
}

// ListPayables returns one page, oldest receipt first, and the summary of the
// open payables the filter matches.
func (r *PostgresRepository) ListPayables(ctx context.Context, garageID string, f usecase.PayableFilter) ([]*domain.Payable, int, usecase.PayableSummary, error) {
	where := `WHERE garage_id = $1 AND status <> 'cancelled'
		AND ($2 = '' OR supplier_id = NULLIF($2, '')::uuid)
		AND ($3 OR status = 'open')`
	args := []any{garageID, f.SupplierID, f.IncludePaid}

	var (
		total   int
		summary usecase.PayableSummary
	)

	err := r.q(ctx).QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'open'),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE status = 'open'), 0)
		FROM supplier_payables `+where, args...).Scan(&total, &summary.OpenCount, &summary.OutstandingAmount)
	if err != nil {
		return nil, 0, usecase.PayableSummary{}, fmt.Errorf("count supplier payables: %w", err)
	}

	if total == 0 {
		return nil, 0, summary, nil
	}

	rows, err := r.q(ctx).Query(ctx, `SELECT `+payableColumns+` FROM supplier_payables `+where+`
		ORDER BY receipt_date, created_at, id LIMIT $4 OFFSET $5`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, usecase.PayableSummary{}, fmt.Errorf("list supplier payables: %w", err)
	}
	defer rows.Close()

	items := make([]*domain.Payable, 0, f.Limit)

	for rows.Next() {
		p, err := scanPayable(rows)
		if err != nil {
			return nil, 0, usecase.PayableSummary{}, fmt.Errorf("scan supplier payable: %w", err)
		}

		items = append(items, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, usecase.PayableSummary{}, fmt.Errorf("list supplier payables: %w", err)
	}

	return items, total, summary, nil
}

// Aging returns, per supplier, the remaining amount of the open payables by
// age of the receipt as of today.
func (r *PostgresRepository) Aging(ctx context.Context, garageID string, today time.Time) ([]domain.Aging, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT supplier_id::text, max(supplier_name),
			COALESCE(sum(total_amount - paid_amount), 0),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE age <= 30), 0),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE age BETWEEN 31 AND 60), 0),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE age BETWEEN 61 AND 90), 0),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE age > 90), 0)
		FROM (SELECT *, GREATEST($2::date - receipt_date, 0) AS age FROM supplier_payables
			WHERE garage_id = $1 AND status = 'open') p
		GROUP BY supplier_id ORDER BY max(supplier_name), supplier_id`, garageID, today)
	if err != nil {
		return nil, fmt.Errorf("age supplier payables: %w", err)
	}
	defer rows.Close()

	var out []domain.Aging

	for rows.Next() {
		var a domain.Aging
		if err := rows.Scan(&a.SupplierID, &a.SupplierName, &a.Total, &a.Current, &a.Days31to60, &a.Days61to90, &a.Over90); err != nil {
			return nil, fmt.Errorf("scan supplier aging: %w", err)
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

// LockPayables returns the payables of the ids, locked until the end of the
// transaction, in id order.
func (r *PostgresRepository) LockPayables(ctx context.Context, garageID string, ids []string) ([]*domain.Payable, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT `+payableColumns+` FROM supplier_payables
		WHERE garage_id = $1 AND id = ANY($2::uuid[]) ORDER BY id FOR UPDATE`, garageID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock supplier payables: %w", err)
	}
	defer rows.Close()

	var out []*domain.Payable

	for rows.Next() {
		p, err := scanPayable(rows)
		if err != nil {
			return nil, fmt.Errorf("scan supplier payable: %w", err)
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// SavePayable writes the paid amount and status of the payable.
func (r *PostgresRepository) SavePayable(ctx context.Context, p *domain.Payable) error {
	_, err := r.q(ctx).Exec(ctx, `UPDATE supplier_payables SET paid_amount = $3, status = $4
		WHERE garage_id = $1 AND id = $2`, p.GarageID, p.ID, p.PaidAmount, string(p.Status))
	if err != nil {
		return fmt.Errorf("save supplier payable: %w", err)
	}

	return nil
}

const voucherColumns = `id, garage_id, number, supplier_id, supplier_name, payee_name, COALESCE(payee_address, ''), amount, method::text,
	reason, COALESCE(note, ''), issuer, issuer_name, COALESCE(void_reason, ''), voided_at, created_at, COALESCE(created_by::text, '')`

func scanVoucher(row pgx.Row) (*domain.PaymentOutVoucher, error) {
	var v domain.PaymentOutVoucher

	err := row.Scan(&v.ID, &v.GarageID, &v.Number, &v.SupplierID, &v.SupplierName, &v.PayeeName, &v.PayeeAddress, &v.Amount,
		&v.Method, &v.Reason, &v.Note, &v.Issuer, &v.IssuerName, &v.VoidReason, &v.VoidedAt, &v.CreatedAt, &v.CreatedBy)
	if err != nil {
		return nil, err
	}

	return &v, nil
}

// AddVoucher inserts the voucher with its allocations.
func (r *PostgresRepository) AddVoucher(ctx context.Context, v *domain.PaymentOutVoucher) error {
	q := r.q(ctx)

	err := q.QueryRow(ctx, `
		INSERT INTO payment_out_vouchers (id, garage_id, number, supplier_id, supplier_name, payee_name, payee_address, amount,
			method, reason, note, issuer, issuer_name, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9::payment_method, $10, NULLIF($11, ''), $12, $13, NULLIF($14, '')::uuid)
		RETURNING created_at`,
		v.ID, v.GarageID, v.Number, v.SupplierID, v.SupplierName, v.PayeeName, v.PayeeAddress, v.Amount, v.Method, v.Reason,
		v.Note, v.Issuer, v.IssuerName, v.CreatedBy).Scan(&v.CreatedAt)
	if err != nil {
		return fmt.Errorf("add payment-out voucher: %w", err)
	}

	for _, a := range v.Allocations {
		_, err := q.Exec(ctx, `INSERT INTO payment_out_allocations (garage_id, voucher_id, payable_id, amount, created_by)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid)`, v.GarageID, v.ID, a.PayableID, a.Amount, v.CreatedBy)
		if err != nil {
			return fmt.Errorf("add payment-out allocation: %w", err)
		}
	}

	return nil
}

// FindVoucher returns the voucher with its allocations.
func (r *PostgresRepository) FindVoucher(ctx context.Context, garageID, voucherID string) (*domain.PaymentOutVoucher, error) {
	v, err := scanVoucher(r.q(ctx).QueryRow(ctx,
		`SELECT `+voucherColumns+` FROM payment_out_vouchers WHERE garage_id = $1 AND id = $2`, garageID, voucherID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrVoucherNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find payment-out voucher: %w", err)
	}

	byVoucher, err := r.allocationsOf(ctx, garageID, []string{v.ID})
	if err != nil {
		return nil, err
	}

	v.Allocations = byVoucher[v.ID]

	return v, nil
}

func (r *PostgresRepository) allocationsOf(ctx context.Context, garageID string, voucherIDs []string) (map[string][]domain.Allocation, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT a.voucher_id::text, a.payable_id::text, p.goods_receipt_number, a.amount, p.status = 'cancelled'
		FROM payment_out_allocations a
		JOIN supplier_payables p ON p.garage_id = a.garage_id AND p.id = a.payable_id
		WHERE a.garage_id = $1 AND a.voucher_id = ANY($2::uuid[]) ORDER BY a.created_at, a.id`, garageID, voucherIDs)
	if err != nil {
		return nil, fmt.Errorf("read payment-out allocations: %w", err)
	}
	defer rows.Close()

	out := map[string][]domain.Allocation{}

	for rows.Next() {
		var (
			voucherID string
			a         domain.Allocation
		)

		if err := rows.Scan(&voucherID, &a.PayableID, &a.GoodsReceiptNumber, &a.Amount, &a.Cancelled); err != nil {
			return nil, fmt.Errorf("scan payment-out allocation: %w", err)
		}

		out[voucherID] = append(out[voucherID], a)
	}

	return out, rows.Err()
}

// ListVouchers returns one page, newest first.
func (r *PostgresRepository) ListVouchers(ctx context.Context, garageID string, f usecase.VoucherFilter) ([]*domain.PaymentOutVoucher, int, error) {
	where := `WHERE garage_id = $1 AND ($2 = '' OR supplier_id = NULLIF($2, '')::uuid) AND ($3 OR voided_at IS NULL)`
	args := []any{garageID, f.SupplierID, f.IncludeVoided}

	var total int
	if err := r.q(ctx).QueryRow(ctx, `SELECT count(*) FROM payment_out_vouchers `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count payment-out vouchers: %w", err)
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := r.q(ctx).Query(ctx, `SELECT `+voucherColumns+` FROM payment_out_vouchers `+where+`
		ORDER BY created_at DESC, id LIMIT $4 OFFSET $5`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list payment-out vouchers: %w", err)
	}

	items := make([]*domain.PaymentOutVoucher, 0, f.Limit)
	ids := make([]string, 0, f.Limit)

	for rows.Next() {
		v, err := scanVoucher(rows)
		if err != nil {
			rows.Close()

			return nil, 0, fmt.Errorf("scan payment-out voucher: %w", err)
		}

		items = append(items, v)
		ids = append(ids, v.ID)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list payment-out vouchers: %w", err)
	}

	allocations, err := r.allocationsOf(ctx, garageID, ids)
	if err != nil {
		return nil, 0, err
	}

	for _, v := range items {
		v.Allocations = allocations[v.ID]
	}

	return items, total, nil
}

// VoidVoucher marks an unvoided voucher voided.
func (r *PostgresRepository) VoidVoucher(ctx context.Context, garageID, voucherID, reason string, at time.Time) error {
	tag, err := r.q(ctx).Exec(ctx, `UPDATE payment_out_vouchers SET void_reason = $3, voided_at = $4
		WHERE garage_id = $1 AND id = $2 AND voided_at IS NULL`, garageID, voucherID, reason, at)
	if err != nil {
		return fmt.Errorf("void payment-out voucher: %w", err)
	}

	if tag.RowsAffected() > 0 {
		return nil
	}

	var exists bool

	err = r.q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payment_out_vouchers WHERE garage_id = $1 AND id = $2)`,
		garageID, voucherID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("find payment-out voucher: %w", err)
	}

	if !exists {
		return domain.ErrVoucherNotFound
	}

	return domain.ErrPaymentOutAlreadyVoided
}

// PaidOut totals the payment-out vouchers issued in [from, to) and not
// voided, for the reconciliation.
func (r *PostgresRepository) PaidOut(ctx context.Context, garageID string, from, to time.Time) (int64, error) {
	var sum int64

	err := r.q(ctx).QueryRow(ctx, `SELECT COALESCE(sum(amount), 0) FROM payment_out_vouchers
		WHERE garage_id = $1 AND voided_at IS NULL AND created_at >= $2 AND created_at < $3`, garageID, from, to).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("total payment-out vouchers: %w", err)
	}

	return sum, nil
}
