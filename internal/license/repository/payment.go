package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const requestColumns = `r.id, r.user_id, r.subscription_id, r.kind::text, r.plan_id, r.status::text, r.reference,
	r.quoted_amount, r.credit_applied, r.amount_due, r.credit_surplus, r.unused_credit, r.period_days, r.unused_days, r.instructions,
	r.payer_name, r.payer_bank, r.payer_account, r.transferred_amount, r.bank_transaction_ref, r.paid_at,
	r.evidence_file_id::text, r.amount_mismatch, r.review_note, r.rejection_reason, r.reviewed_by::text, r.reviewed_at,
	r.submitted_at, r.created_at, ` + planColumns

const requestFrom = ` FROM plan_payment_requests r JOIN plans p ON p.id = r.plan_id`

// nullableText maps SQL NULL text to "".
type nullableText struct{ v *string }

func (n nullableText) String() string {
	if n.v == nil {
		return ""
	}

	return *n.v
}

func scanRequest(row pgx.Row) (*domain.PlanPaymentRequest, error) {
	var (
		r                                                                                domain.PlanPaymentRequest
		plan                                                                             domain.Plan
		kind, status                                                                     string
		instructions                                                                     []byte
		payerName, payerBank, payerAccount, txRef, evidence, note, rejection, reviewedBy *string
	)

	planFields, finish := planDest(&plan)
	dest := append([]any{
		&r.ID, &r.UserID, &r.SubscriptionID, &kind, &r.PlanID, &status, &r.Reference,
		&r.QuotedAmount, &r.CreditApplied, &r.AmountDue, &r.CreditSurplus, &r.UnusedCredit, &r.PeriodDays, &r.UnusedDays, &instructions,
		&payerName, &payerBank, &payerAccount, &r.TransferredAmount, &txRef, &r.PaidAt,
		&evidence, &r.AmountMismatch, &note, &rejection, &reviewedBy, &r.ReviewedAt,
		&r.SubmittedAt, &r.CreatedAt,
	}, planFields...)

	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPaymentRequestNotFound
		}

		return nil, err
	}

	finish()

	if len(instructions) > 0 {
		if err := json.Unmarshal(instructions, &r.Instructions); err != nil {
			return nil, err
		}
	}

	r.Kind, r.Status, r.Plan = domain.PaymentKind(kind), domain.PaymentStatus(status), &plan
	r.PayerName, r.PayerBank, r.PayerAccount = nullableText{payerName}.String(), nullableText{payerBank}.String(), nullableText{payerAccount}.String()
	r.BankTransactionRef, r.EvidenceFileID = nullableText{txRef}.String(), nullableText{evidence}.String()
	r.ReviewNote, r.RejectionReason, r.ReviewedBy = nullableText{note}.String(), nullableText{rejection}.String(), nullableText{reviewedBy}.String()

	return &r, nil
}

// CreateRequest inserts r.
func (r *PostgresRepository) CreateRequest(ctx context.Context, req *domain.PlanPaymentRequest) error {
	instructions, err := json.Marshal(req.Instructions)
	if err != nil {
		return err
	}

	err = platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO plan_payment_requests (user_id, subscription_id, kind, plan_id, status, reference, quoted_amount,
			credit_applied, amount_due, credit_surplus, unused_credit, period_days, unused_days, instructions)
		VALUES ($1, $2, $3::plan_payment_kind, $4, $5::plan_payment_status, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, created_at`,
		req.UserID, req.SubscriptionID, string(req.Kind), req.PlanID, string(req.Status), req.Reference, req.QuotedAmount,
		req.CreditApplied, req.AmountDue, req.CreditSurplus, req.UnusedCredit, req.PeriodDays, req.UnusedDays, instructions,
	).Scan(&req.ID, &req.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "idx_plan_payment_requests_open":
			return domain.ErrRequestAlreadyOpen
		case "plan_payment_requests_reference_key":
			return domain.ErrReferenceTaken
		}
	}

	return err
}

// FindRequest returns the request with its plan.
func (r *PostgresRepository) FindRequest(ctx context.Context, id string) (*domain.PlanPaymentRequest, error) {
	if uuid.Validate(id) != nil {
		return nil, domain.ErrPaymentRequestNotFound
	}

	return scanRequest(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+requestColumns+requestFrom+` WHERE r.id = $1`, id))
}

// LockRequest reads the request holding a row lock.
func (r *PostgresRepository) LockRequest(ctx context.Context, id string) (*domain.PlanPaymentRequest, error) {
	if uuid.Validate(id) != nil {
		return nil, domain.ErrPaymentRequestNotFound
	}

	return scanRequest(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+requestColumns+requestFrom+` WHERE r.id = $1 FOR UPDATE OF r`, id))
}

// FindOpenRequest returns the owner's open request.
func (r *PostgresRepository) FindOpenRequest(ctx context.Context, userID string) (*domain.PlanPaymentRequest, error) {
	return scanRequest(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+requestColumns+requestFrom+` WHERE r.user_id = $1 AND r.status IN ('awaiting_payment', 'submitted')`, userID))
}

// SaveRequest persists the status, declaration and review fields.
func (r *PostgresRepository) SaveRequest(ctx context.Context, req *domain.PlanPaymentRequest) error {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		UPDATE plan_payment_requests SET status = $2::plan_payment_status, payer_name = $3, payer_bank = $4,
			payer_account = $5, transferred_amount = $6, bank_transaction_ref = $7, paid_at = $8,
			evidence_file_id = $9::uuid, amount_mismatch = $10, review_note = $11, rejection_reason = $12,
			reviewed_by = $13::uuid, reviewed_at = $14, submitted_at = $15
		WHERE id = $1`,
		req.ID, string(req.Status), nullableString(req.PayerName), nullableString(req.PayerBank),
		nullableString(req.PayerAccount), req.TransferredAmount, nullableString(req.BankTransactionRef), req.PaidAt,
		nullableString(req.EvidenceFileID), req.AmountMismatch, nullableString(req.ReviewNote),
		nullableString(req.RejectionReason), nullableString(req.ReviewedBy), req.ReviewedAt, req.SubmittedAt)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrPaymentRequestNotFound
	}

	return nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// ListRequests returns the owner's requests, newest first.
func (r *PostgresRepository) ListRequests(
	ctx context.Context, userID string, status domain.PaymentStatus,
) ([]*domain.PlanPaymentRequest, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT `+requestColumns+requestFrom+`
		WHERE r.user_id = $1 AND ($2 = '' OR r.status::text = $2)
		ORDER BY r.created_at DESC`, userID, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.PlanPaymentRequest

	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, req)
	}

	return out, rows.Err()
}

// SearchRequests is the review queue: oldest first (newest first for the
// other statuses).
func (r *PostgresRepository) SearchRequests(ctx context.Context, f usecase.ReviewFilter) (*usecase.ReviewPage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	from := requestFrom + ` JOIN users u ON u.id = r.user_id`
	where := ` WHERE r.status::text = $1 AND ($2 = '' OR lower(u.email) LIKE $2 OR lower(u.full_name) LIKE $2 OR lower(r.reference) LIKE $2)`
	pattern := ""

	if query := strings.ToLower(strings.TrimSpace(f.Query)); query != "" {
		pattern = "%" + query + "%"
	}

	order := "r.submitted_at ASC NULLS LAST, r.created_at"
	if f.Status != domain.PaymentSubmitted {
		order = "r.created_at DESC"
	}

	page := &usecase.ReviewPage{}
	if err := q.QueryRow(ctx, `SELECT count(*)`+from+where, string(f.Status), pattern).Scan(&page.Total); err != nil {
		return nil, err
	}

	rows, err := q.Query(ctx, `SELECT `+requestColumns+`, COALESCE(u.email, ''), u.full_name`+from+where+
		` ORDER BY `+order+` LIMIT $3 OFFSET $4`, string(f.Status), pattern, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item usecase.ReviewItem

		req, err := scanRequestWith(rows, &item.Email, &item.FullName)
		if err != nil {
			return nil, err
		}

		item.Request = req
		page.Items = append(page.Items, item)
	}

	return page, rows.Err()
}

// scanRequestWith is scanRequest with trailing columns.
func scanRequestWith(row pgx.Rows, extra ...any) (*domain.PlanPaymentRequest, error) {
	return scanRequest(rowWithExtra{Rows: row, extra: extra})
}

// rowWithExtra scans the request columns plus extra destinations.
type rowWithExtra struct {
	pgx.Rows

	extra []any
}

func (r rowWithExtra) Scan(dest ...any) error {
	return r.Rows.Scan(append(dest, r.extra...)...)
}

// WaitingForReview counts the submitted requests and the oldest one.
func (r *PostgresRepository) WaitingForReview(ctx context.Context) (int, time.Time, error) {
	var (
		count  int
		oldest *time.Time
	)

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*), min(submitted_at) FROM plan_payment_requests WHERE status = 'submitted'`).Scan(&count, &oldest)
	if err != nil {
		return 0, time.Time{}, err
	}

	if oldest == nil {
		return count, time.Time{}, nil
	}

	return count, *oldest, nil
}

// AddCreditEntry appends to the ledger.
func (r *PostgresRepository) AddCreditEntry(ctx context.Context, e *domain.CreditEntry) error {
	return platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO credit_ledger (user_id, kind, amount, balance_after, plan_payment_request_id, note, created_by)
		VALUES ($1, $2::credit_entry_kind, $3, $4, $5::uuid, $6, $7::uuid)
		RETURNING id, created_at`,
		e.UserID, string(e.Kind), e.Amount, e.BalanceAfter, nullableString(e.PlanPaymentRequestID), nullableString(e.Note),
		nullableString(e.CreatedBy),
	).Scan(&e.ID, &e.CreatedAt)
}

// ListCredit returns the owner's ledger, newest first.
func (r *PostgresRepository) ListCredit(ctx context.Context, userID string) ([]*domain.CreditEntry, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT id, user_id, kind::text, amount, balance_after, plan_payment_request_id::text, note, created_by::text, created_at
		FROM credit_ledger WHERE user_id = $1 ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.CreditEntry

	for rows.Next() {
		var (
			e                   domain.CreditEntry
			kind                string
			requestID, note, by *string
		)

		if err := rows.Scan(&e.ID, &e.UserID, &kind, &e.Amount, &e.BalanceAfter, &requestID, &note, &by, &e.CreatedAt); err != nil {
			return nil, err
		}

		e.Kind = domain.CreditEntryKind(kind)
		e.PlanPaymentRequestID, e.Note, e.CreatedBy = nullableText{requestID}.String(), nullableText{note}.String(), nullableText{by}.String()
		out = append(out, &e)
	}

	return out, rows.Err()
}

// Settings returns the platform's payment details.
func (r *PostgresRepository) Settings(ctx context.Context) (*domain.PaymentSettings, error) {
	var (
		s  domain.PaymentSettings
		qr *string
	)

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT bank_name, account_number, account_holder, branch, instructions, qr_file_id::text, updated_at
		FROM platform_payment_settings WHERE id = 1`).
		Scan(&s.BankName, &s.AccountNumber, &s.AccountHolder, &s.Branch, &s.TransferInstructions, &qr, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}

	s.QRFileID = nullableText{qr}.String()

	return &s, nil
}

// SaveSettings persists the details and the QR file.
func (r *PostgresRepository) SaveSettings(ctx context.Context, s *domain.PaymentSettings, updatedBy string) error {
	return platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE platform_payment_settings SET bank_name = $1, account_number = $2, account_holder = $3, branch = $4,
			instructions = $5, qr_file_id = $6::uuid, updated_by = $7::uuid
		WHERE id = 1
		RETURNING updated_at`,
		s.BankName, s.AccountNumber, s.AccountHolder, s.Branch, s.TransferInstructions, nullableString(s.QRFileID),
		nullableString(updatedBy)).Scan(&s.UpdatedAt)
}

// ClaimReviewReminder reports whether the admins are due a reminder and, if
// so, records it: at most once per minInterval, whichever instance asks.
func (r *PostgresRepository) ClaimReviewReminder(ctx context.Context, now time.Time, minInterval time.Duration) (bool, error) {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		UPDATE platform_payment_settings SET last_review_reminder_at = $1
		WHERE id = 1 AND (last_review_reminder_at IS NULL OR last_review_reminder_at <= $2)`,
		now, now.Add(-minInterval))
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

// OpenRequests returns the open request of each of the subscriptions.
func (r *PostgresRepository) OpenRequests(ctx context.Context, subscriptionIDs []string) (map[string]*domain.PlanPaymentRequest, error) {
	out := map[string]*domain.PlanPaymentRequest{}
	if len(subscriptionIDs) == 0 {
		return out, nil
	}

	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT `+requestColumns+requestFrom+`
		WHERE r.subscription_id = ANY($1::uuid[]) AND r.status IN ('awaiting_payment', 'submitted')`, subscriptionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}

		out[req.SubscriptionID] = req
	}

	return out, rows.Err()
}
