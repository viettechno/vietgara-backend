// Package repository implements the persistence layer of the Receivables
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of row-level security, and none reads another
// module's tables: the customer name and the plate are denormalized on the
// receivable itself.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

// PostgresRepository implements usecase.Repository over the shared pgx pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a receivable repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, r.pool)
}

// GetSettings returns the garage's settings, the defaults when it has none.
func (r *PostgresRepository) GetSettings(ctx context.Context, garageID string) (domain.Settings, error) {
	var (
		s       domain.Settings
		offsets []int32
	)

	err := r.q(ctx).QueryRow(ctx, `
		SELECT payment_terms_days, reminders_enabled, reminder_offset_days
		FROM receivable_settings WHERE garage_id = $1`, garageID).Scan(&s.PaymentTermsDays, &s.RemindersEnabled, &offsets)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultSettings(), nil
	}

	if err != nil {
		return domain.Settings{}, fmt.Errorf("read receivable settings: %w", err)
	}

	for _, o := range offsets {
		s.ReminderOffsetDays = append(s.ReminderOffsetDays, int(o))
	}

	return s, nil
}

// SaveSettings inserts or replaces the garage's settings.
func (r *PostgresRepository) SaveSettings(ctx context.Context, garageID string, s domain.Settings) error {
	offsets := make([]int32, len(s.ReminderOffsetDays))
	for i, o := range s.ReminderOffsetDays {
		offsets[i] = int32(o) //nolint:gosec // validated to 1..90
	}

	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO receivable_settings (garage_id, payment_terms_days, reminders_enabled, reminder_offset_days)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (garage_id) DO UPDATE SET payment_terms_days = EXCLUDED.payment_terms_days,
			reminders_enabled = EXCLUDED.reminders_enabled, reminder_offset_days = EXCLUDED.reminder_offset_days`,
		garageID, s.PaymentTermsDays, s.RemindersEnabled, offsets)
	if err != nil {
		return fmt.Errorf("save receivable settings: %w", err)
	}

	return nil
}

const receivableColumns = `id, garage_id, settlement_id, settlement_number, customer_id, customer_name, vehicle_license_plate,
	total_amount, paid_amount, due_date, status, reminders_sent, last_reminder_at, created_at, updated_at`

func scanReceivable(row pgx.Row) (*domain.Receivable, error) {
	var (
		rec        domain.Receivable
		customerID *string
		status     string
	)

	err := row.Scan(&rec.ID, &rec.GarageID, &rec.SettlementID, &rec.SettlementNumber, &customerID, &rec.CustomerName,
		&rec.VehicleLicensePlate, &rec.TotalAmount, &rec.PaidAmount, &rec.DueDate, &status, &rec.RemindersSent,
		&rec.LastReminderAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if customerID != nil {
		rec.CustomerID = *customerID
	}

	rec.Status = domain.Status(status)

	return &rec, nil
}

// FindBySettlement returns the receivable of a settlement without its ledger.
func (r *PostgresRepository) FindBySettlement(ctx context.Context, garageID, settlementID string) (*domain.Receivable, error) {
	rec, err := scanReceivable(r.q(ctx).QueryRow(ctx,
		`SELECT `+receivableColumns+` FROM receivables WHERE garage_id = $1 AND settlement_id = $2`, garageID, settlementID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReceivableNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find receivable of settlement: %w", err)
	}

	return rec, nil
}

// FindByID returns the receivable with its ledger and reminders.
func (r *PostgresRepository) FindByID(ctx context.Context, garageID, receivableID string) (*domain.Receivable, error) {
	rec, err := scanReceivable(r.q(ctx).QueryRow(ctx,
		`SELECT `+receivableColumns+` FROM receivables WHERE garage_id = $1 AND id = $2`, garageID, receivableID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReceivableNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find receivable: %w", err)
	}

	if rec.Entries, err = r.entriesOf(ctx, garageID, rec.ID); err != nil {
		return nil, err
	}

	if rec.Reminders, err = r.remindersOf(ctx, garageID, rec.ID); err != nil {
		return nil, err
	}

	return rec, nil
}

func (r *PostgresRepository) entriesOf(ctx context.Context, garageID, receivableID string) ([]domain.Entry, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT id, kind, amount, reference, method::text, source_key, occurred_at
		FROM receivable_entries WHERE garage_id = $1 AND receivable_id = $2 ORDER BY occurred_at, created_at, id`, garageID, receivableID)
	if err != nil {
		return nil, fmt.Errorf("read receivable entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.Entry

	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan receivable entry: %w", err)
		}

		entries = append(entries, e)
	}

	return entries, rows.Err()
}

func scanEntry(row pgx.Row) (domain.Entry, error) {
	var (
		e      domain.Entry
		kind   string
		method *string
	)

	if err := row.Scan(&e.ID, &kind, &e.Amount, &e.Reference, &method, &e.SourceKey, &e.OccurredAt); err != nil {
		return domain.Entry{}, err
	}

	e.Kind = domain.EntryKind(kind)
	if method != nil {
		e.Method = *method
	}

	return e, nil
}

func (r *PostgresRepository) remindersOf(ctx context.Context, garageID, receivableID string) ([]domain.Reminder, error) {
	byReceivable, err := r.remindersOfAll(ctx, garageID, []string{receivableID})

	return byReceivable[receivableID], err
}

// remindersOfAll reads the reminders of several receivables at once.
func (r *PostgresRepository) remindersOfAll(ctx context.Context, garageID string, receivableIDs []string) (map[string][]domain.Reminder, error) {
	out := map[string][]domain.Reminder{}
	if len(receivableIDs) == 0 {
		return out, nil
	}

	rows, err := r.q(ctx).Query(ctx, `
		SELECT receivable_id::text, id, offset_days, channel, outcome, COALESCE(reason, ''), attempts, created_at, updated_at
		FROM receivable_reminders WHERE garage_id = $1 AND receivable_id = ANY($2::uuid[]) ORDER BY receivable_id, offset_days`,
		garageID, receivableIDs)
	if err != nil {
		return nil, fmt.Errorf("read receivable reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			receivableID string
			rem          domain.Reminder
			outcome      string
		)

		if err := rows.Scan(&receivableID, &rem.ID, &rem.OffsetDays, &rem.Channel, &outcome, &rem.Reason, &rem.Attempts, &rem.CreatedAt, &rem.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan receivable reminder: %w", err)
		}

		rem.Outcome = domain.Outcome(outcome)
		out[receivableID] = append(out[receivableID], rem)
	}

	return out, rows.Err()
}

// Save inserts the receivable, or updates the amounts, status and customer of
// the one of the same settlement.
func (r *PostgresRepository) Save(ctx context.Context, rec *domain.Receivable) error {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO receivables (garage_id, settlement_id, settlement_number, customer_id, customer_name, vehicle_license_plate,
			total_amount, paid_amount, due_date, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (garage_id, settlement_id) DO UPDATE SET settlement_number = EXCLUDED.settlement_number,
			customer_id = COALESCE(EXCLUDED.customer_id, receivables.customer_id), customer_name = EXCLUDED.customer_name,
			vehicle_license_plate = EXCLUDED.vehicle_license_plate, total_amount = EXCLUDED.total_amount,
			paid_amount = EXCLUDED.paid_amount, status = EXCLUDED.status
		RETURNING id, created_at, updated_at`,
		rec.GarageID, rec.SettlementID, rec.SettlementNumber, nullableString(rec.CustomerID), rec.CustomerName,
		rec.VehicleLicensePlate, rec.TotalAmount, rec.PaidAmount, rec.DueDate, string(rec.Status),
	).Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save receivable: %w", err)
	}

	return nil
}

// AddEntries inserts the ledger entries that are not there yet, by source key.
func (r *PostgresRepository) AddEntries(ctx context.Context, garageID, receivableID string, entries []domain.Entry) error {
	for _, e := range entries {
		_, err := r.q(ctx).Exec(ctx, `
			INSERT INTO receivable_entries (garage_id, receivable_id, kind, amount, reference, method, source_key, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6::payment_method, $7, $8)
			ON CONFLICT (garage_id, source_key) DO NOTHING`,
			garageID, receivableID, string(e.Kind), e.Amount, e.Reference, nullableString(e.Method), e.SourceKey, e.OccurredAt)
		if err != nil {
			return fmt.Errorf("add receivable entry: %w", err)
		}
	}

	return nil
}

// List returns one page, most overdue first, and the summary of every open
// receivable the filter matches.
func (r *PostgresRepository) List(ctx context.Context, garageID string, f usecase.ListFilter) ([]*domain.Receivable, int, usecase.Summary, error) {
	where := `WHERE garage_id = $1
		AND ($2 = '' OR customer_id = NULLIF($2, '')::uuid)
		AND (NOT $3 OR (status = 'open' AND due_date < $4::date))
		AND ($5 OR status = 'open')
		AND ($6 = '' OR settlement_number ILIKE '%' || $6 || '%' OR customer_name ILIKE '%' || $6 || '%'
			OR vehicle_license_plate ILIKE '%' || $6 || '%')`
	args := []any{garageID, f.CustomerID, f.OverdueOnly, f.Today, f.IncludePaid, escapeLike(f.Query)}

	q := r.q(ctx)

	var (
		total   int
		summary usecase.Summary
	)

	err := q.QueryRow(ctx, `
		SELECT count(*),
			count(*) FILTER (WHERE status = 'open'),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE status = 'open'), 0),
			count(*) FILTER (WHERE status = 'open' AND due_date < $4::date),
			COALESCE(sum(total_amount - paid_amount) FILTER (WHERE status = 'open' AND due_date < $4::date), 0)
		FROM receivables `+where, args...).
		Scan(&total, &summary.OpenCount, &summary.OutstandingAmount, &summary.OverdueCount, &summary.OverdueAmount)
	if err != nil {
		return nil, 0, usecase.Summary{}, fmt.Errorf("count receivables: %w", err)
	}

	if total == 0 {
		return nil, 0, summary, nil
	}

	rows, err := q.Query(ctx, `SELECT `+receivableColumns+` FROM receivables `+where+`
		ORDER BY (status = 'open') DESC, due_date, created_at, id LIMIT $7 OFFSET $8`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, usecase.Summary{}, fmt.Errorf("list receivables: %w", err)
	}
	defer rows.Close()

	items := make([]*domain.Receivable, 0, f.Limit)

	for rows.Next() {
		rec, err := scanReceivable(rows)
		if err != nil {
			return nil, 0, usecase.Summary{}, fmt.Errorf("scan receivable: %w", err)
		}

		items = append(items, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, usecase.Summary{}, fmt.Errorf("list receivables: %w", err)
	}

	return items, total, summary, nil
}

// escapeLike makes a search text match literally in an ILIKE pattern.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ReminderGarages returns the garages with an open receivable that has a
// customer, through the function that sees across garages.
func (r *PostgresRepository) ReminderGarages(ctx context.Context) ([]string, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT receivable_reminder_garages()::text`)
	if err != nil {
		return nil, fmt.Errorf("list reminder garages: %w", err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan reminder garage: %w", err)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// OpenWithCustomer returns the garage's open receivables that have a customer
// and are due on or before the given day, with their reminders.
func (r *PostgresRepository) OpenWithCustomer(ctx context.Context, garageID string, dueOnOrBefore time.Time) ([]*domain.Receivable, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT `+receivableColumns+` FROM receivables
		WHERE garage_id = $1 AND status = 'open' AND customer_id IS NOT NULL AND due_date <= $2::date ORDER BY due_date, id`,
		garageID, dueOnOrBefore)
	if err != nil {
		return nil, fmt.Errorf("list open receivables: %w", err)
	}

	var items []*domain.Receivable

	for rows.Next() {
		rec, err := scanReceivable(rows)
		if err != nil {
			rows.Close()

			return nil, fmt.Errorf("scan receivable: %w", err)
		}

		items = append(items, rec)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open receivables: %w", err)
	}

	ids := make([]string, len(items))
	for i, rec := range items {
		ids[i] = rec.ID
	}

	reminders, err := r.remindersOfAll(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	for _, rec := range items {
		rec.Reminders = reminders[rec.ID]
	}

	return items, nil
}

// MissingSettlements returns up to limit settlements that are not paid and
// have no receivable, through the function that sees across garages.
func (r *PostgresRepository) MissingSettlements(ctx context.Context, limit int) ([]usecase.MissingSettlement, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT garage_id::text, settlement_id::text FROM receivable_missing_settlements($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("list missing receivables: %w", err)
	}
	defer rows.Close()

	var missing []usecase.MissingSettlement

	for rows.Next() {
		var m usecase.MissingSettlement
		if err := rows.Scan(&m.GarageID, &m.SettlementID); err != nil {
			return nil, fmt.Errorf("scan missing receivable: %w", err)
		}

		missing = append(missing, m)
	}

	return missing, rows.Err()
}

// LockSettlement takes a transaction-scoped advisory lock on the settlement,
// waiting for it.
func (r *PostgresRepository) LockSettlement(ctx context.Context, settlementID string) error {
	if _, err := r.q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1))`, "receivable-sync:"+settlementID); err != nil {
		return fmt.Errorf("lock settlement %s: %w", settlementID, err)
	}

	return nil
}

// TryLock takes a transaction-scoped advisory lock on key.
func (r *PostgresRepository) TryLock(ctx context.Context, key string) (bool, error) {
	var locked bool
	if err := r.q(ctx).QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, key).Scan(&locked); err != nil {
		return false, fmt.Errorf("lock %s: %w", key, err)
	}

	return locked, nil
}

// SaveReminder inserts the reminder or updates the one of the same receivable
// and offset.
func (r *PostgresRepository) SaveReminder(ctx context.Context, garageID, receivableID string, rem *domain.Reminder) error {
	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO receivable_reminders (garage_id, receivable_id, offset_days, channel, outcome, reason, attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (receivable_id, offset_days) DO UPDATE SET channel = EXCLUDED.channel, outcome = EXCLUDED.outcome,
			reason = EXCLUDED.reason, attempts = EXCLUDED.attempts`,
		garageID, receivableID, rem.OffsetDays, rem.Channel, string(rem.Outcome), nullableString(rem.Reason), rem.Attempts)
	if err != nil {
		return fmt.Errorf("save receivable reminder: %w", err)
	}

	return nil
}

// MarkReminded counts one reminder sent at the given time.
func (r *PostgresRepository) MarkReminded(ctx context.Context, garageID, receivableID string, at time.Time) error {
	_, err := r.q(ctx).Exec(ctx, `
		UPDATE receivables SET reminders_sent = reminders_sent + 1, last_reminder_at = $3
		WHERE garage_id = $1 AND id = $2`, garageID, receivableID, at)
	if err != nil {
		return fmt.Errorf("mark receivable reminded: %w", err)
	}

	return nil
}

const balanceExpr = `CASE kind WHEN 'payment' THEN -amount ELSE amount END`

// BalanceBefore is the ledger's balance of entries before t.
func (r *PostgresRepository) BalanceBefore(ctx context.Context, garageID string, t time.Time) (int64, error) {
	var balance int64

	err := r.q(ctx).QueryRow(ctx, `
		SELECT COALESCE(sum(`+balanceExpr+`), 0) FROM receivable_entries WHERE garage_id = $1 AND occurred_at < $2`,
		garageID, t).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("read opening balance: %w", err)
	}

	return balance, nil
}

// Entries returns the ledger entries in [from, to), oldest first.
func (r *PostgresRepository) Entries(ctx context.Context, garageID string, from, to time.Time) ([]usecase.LedgerLine, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT e.id, e.kind, e.amount, e.reference, e.method::text, e.source_key, e.occurred_at, rc.customer_name
		FROM receivable_entries e
		JOIN receivables rc ON rc.garage_id = e.garage_id AND rc.id = e.receivable_id
		WHERE e.garage_id = $1 AND e.occurred_at >= $2 AND e.occurred_at < $3
		ORDER BY e.occurred_at, e.created_at, e.id`, garageID, from, to)
	if err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	defer rows.Close()

	var lines []usecase.LedgerLine

	for rows.Next() {
		var (
			e      domain.Entry
			kind   string
			method *string
			name   string
		)

		if err := rows.Scan(&e.ID, &kind, &e.Amount, &e.Reference, &method, &e.SourceKey, &e.OccurredAt, &name); err != nil {
			return nil, fmt.Errorf("scan ledger line: %w", err)
		}

		e.Kind = domain.EntryKind(kind)
		if method != nil {
			e.Method = *method
		}

		lines = append(lines, usecase.LedgerLine{Entry: e, CustomerName: name})
	}

	return lines, rows.Err()
}

// LedgerBalance is the balance of the whole ledger.
func (r *PostgresRepository) LedgerBalance(ctx context.Context, garageID string) (int64, error) {
	var balance int64

	err := r.q(ctx).QueryRow(ctx, `
		SELECT COALESCE(sum(`+balanceExpr+`), 0) FROM receivable_entries WHERE garage_id = $1`, garageID).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("read ledger balance: %w", err)
	}

	return balance, nil
}

// RemainingTotal is the sum of the receivables' remaining amounts.
func (r *PostgresRepository) RemainingTotal(ctx context.Context, garageID string) (int64, error) {
	var total int64

	err := r.q(ctx).QueryRow(ctx, `
		SELECT COALESCE(sum(total_amount - paid_amount), 0) FROM receivables WHERE garage_id = $1`, garageID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("read remaining total: %w", err)
	}

	return total, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
