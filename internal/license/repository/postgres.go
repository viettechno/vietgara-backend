// Package repository implements the persistence of the License Management
// module: plans, subscriptions and entitlements in Postgres, and the
// entitlement cache and daily transaction counters in Redis or in memory.
// The license tables are platform data without Row-Level Security
// (Database Design Section 6.3).
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const (
	planColumns = `p.id, p.code, p.name, p.description, p.price, p.currency, p.billing_cycle,
		p.max_garages, p.max_staff, p.max_transactions_per_day, p.features::text[], p.is_public, p.is_active,
		p.sort_order, p.created_at, p.updated_at`

	subscriptionColumns = `s.id, s.user_id, s.plan_id, s.status, s.trial_end_at,
		s.current_period_start, s.current_period_end, s.grace_period_end, s.auto_renew,
		s.reminder_period_end, s.period_paid_amount, s.credit_balance, s.created_at, s.updated_at, ` + planColumns

	subscriptionFrom = ` FROM subscriptions s JOIN plans p ON p.id = s.plan_id`
)

// PostgresRepository implements the usecase Subscription and Entitlement
// repositories over the shared pgx pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a subscription repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// PostgresPlanRepository implements usecase.PlanRepository.
type PostgresPlanRepository struct {
	pool *platformdb.Pool
}

// NewPostgresPlanRepository returns a plan repository backed by pool.
func NewPostgresPlanRepository(pool *platformdb.Pool) *PostgresPlanRepository {
	return &PostgresPlanRepository{pool: pool}
}

// planDest returns scan destinations for planColumns filling p; finish
// completes the fields that need decoding.
func planDest(p *domain.Plan) (dest []any, finish func()) {
	var (
		cycle    string
		features []string
	)

	dest = []any{
		&p.ID, &p.Code, &p.Name, &p.Description, &p.Price, &p.Currency, &cycle,
		&p.MaxGarages, &p.MaxStaff, &p.MaxTransactionsPerDay, &features, &p.Public, &p.Active,
		&p.SortOrder, &p.CreatedAt, &p.UpdatedAt,
	}

	return dest, func() {
		p.BillingCycle = domain.BillingCycle(cycle)

		p.Features = make([]domain.Feature, 0, len(features))
		for _, f := range features {
			p.Features = append(p.Features, domain.Feature(f))
		}
	}
}

func scanPlan(row pgx.Row) (*domain.Plan, error) {
	var p domain.Plan

	dest, finish := planDest(&p)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPlanNotFound
		}

		return nil, err
	}

	finish()

	return &p, nil
}

func subscriptionDest(s *domain.Subscription, plan *domain.Plan, status *string) ([]any, func()) {
	planFields, finish := planDest(plan)

	return append([]any{
		&s.ID, &s.UserID, &s.PlanID, status, &s.TrialEndAt,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.GracePeriodEnd, &s.AutoRenew,
		&s.ReminderPeriodEnd, &s.PeriodPaidAmount, &s.CreditBalance, &s.CreatedAt, &s.UpdatedAt,
	}, planFields...), finish
}

func scanSubscription(row pgx.Row) (*domain.Subscription, error) {
	var (
		s      domain.Subscription
		plan   domain.Plan
		status string
	)

	dest, finish := subscriptionDest(&s, &plan, &status)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSubscriptionNotFound
		}

		return nil, err
	}

	finish()

	s.Status, s.Plan = domain.SubscriptionStatus(status), &plan

	return &s, nil
}

func (r *PostgresPlanRepository) listPlans(ctx context.Context, where string) ([]*domain.Plan, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx,
		`SELECT `+planColumns+` FROM plans p `+where+` ORDER BY p.sort_order, p.price`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*domain.Plan

	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}

		plans = append(plans, p)
	}

	return plans, rows.Err()
}

// ListPublic returns the plans owners may choose, in catalogue order.
func (r *PostgresPlanRepository) ListPublic(ctx context.Context) ([]*domain.Plan, error) {
	return r.listPlans(ctx, `WHERE p.is_public AND p.is_active AND p.code <> 'trial'`)
}

// ListAll returns every plan.
func (r *PostgresPlanRepository) ListAll(ctx context.Context) ([]*domain.Plan, error) {
	return r.listPlans(ctx, ``)
}

// FindByID returns the plan, or domain.ErrPlanNotFound.
func (r *PostgresPlanRepository) FindByID(ctx context.Context, planID string) (*domain.Plan, error) {
	return scanPlan(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans p WHERE p.id = $1`, planID))
}

// FindByCode returns the plan, or domain.ErrPlanNotFound.
func (r *PostgresPlanRepository) FindByCode(ctx context.Context, code string) (*domain.Plan, error) {
	return scanPlan(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans p WHERE p.code = $1`, code))
}

func featureStrings(features []domain.Feature) []string {
	out := make([]string, 0, len(features))
	for _, f := range features {
		out = append(out, string(f))
	}

	return out
}

// Create inserts p.
func (r *PostgresPlanRepository) Create(ctx context.Context, p *domain.Plan) error {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO plans (code, name, description, price, currency, billing_cycle, max_garages, max_staff,
			max_transactions_per_day, features, is_public, is_active, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::plan_feature[], $11, $12, $13)
		RETURNING id, created_at, updated_at`,
		p.Code, p.Name, p.Description, p.Price, p.Currency, string(p.BillingCycle), p.MaxGarages, p.MaxStaff,
		p.MaxTransactionsPerDay, featureStrings(p.Features), p.Public, p.Active, p.SortOrder,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrPlanCodeTaken
	}

	return err
}

// Update persists every field of p but its code.
func (r *PostgresPlanRepository) Update(ctx context.Context, p *domain.Plan) error {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE plans SET name = $2, description = $3, price = $4, currency = $5, billing_cycle = $6, max_garages = $7,
			max_staff = $8, max_transactions_per_day = $9, features = $10::plan_feature[], is_public = $11,
			is_active = $12, sort_order = $13, updated_at = now()
		WHERE id = $1
		RETURNING updated_at`,
		p.ID, p.Name, p.Description, p.Price, p.Currency, string(p.BillingCycle), p.MaxGarages, p.MaxStaff,
		p.MaxTransactionsPerDay, featureStrings(p.Features), p.Public, p.Active, p.SortOrder,
	).Scan(&p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrPlanNotFound
	}

	return err
}

// CreateIfAbsent inserts s unless the user has a subscription already.
func (r *PostgresRepository) CreateIfAbsent(ctx context.Context, s *domain.Subscription) (bool, error) {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO subscriptions (user_id, plan_id, status, trial_end_at, current_period_start,
			current_period_end, auto_renew)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING id, created_at, updated_at`,
		s.UserID, s.PlanID, string(s.Status), s.TrialEndAt, s.CurrentPeriodStart, s.CurrentPeriodEnd, s.AutoRenew,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	return err == nil, err
}

// FindByUserID returns the user's subscription with its plan.
func (r *PostgresRepository) FindByUserID(ctx context.Context, userID string) (*domain.Subscription, error) {
	return scanSubscription(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+subscriptionColumns+subscriptionFrom+` WHERE s.user_id = $1`, userID))
}

// LockByUserID reads the subscription holding a row lock.
func (r *PostgresRepository) LockByUserID(ctx context.Context, userID string) (*domain.Subscription, error) {
	return scanSubscription(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+subscriptionColumns+subscriptionFrom+` WHERE s.user_id = $1 FOR UPDATE OF s`, userID))
}

// LockByID reads a subscription by id holding a row lock.
func (r *PostgresRepository) LockByID(ctx context.Context, id string) (*domain.Subscription, error) {
	return scanSubscription(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+subscriptionColumns+subscriptionFrom+` WHERE s.id = $1 FOR UPDATE OF s`, id))
}

// Update persists the mutable fields of s.
func (r *PostgresRepository) Update(ctx context.Context, s *domain.Subscription) error {
	return platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE subscriptions SET plan_id = $2, status = $3, trial_end_at = $4, current_period_start = $5,
			current_period_end = $6, grace_period_end = $7, auto_renew = $8, reminder_period_end = $9,
			period_paid_amount = $10, credit_balance = $11
		WHERE id = $1
		RETURNING updated_at`,
		s.ID, s.PlanID, string(s.Status), s.TrialEndAt, s.CurrentPeriodStart, s.CurrentPeriodEnd,
		s.GracePeriodEnd, s.AutoRenew, s.ReminderPeriodEnd, s.PeriodPaidAmount, s.CreditBalance,
	).Scan(&s.UpdatedAt)
}

// ListUserIDsDueBefore returns users with a live subscription ending
// before t.
func (r *PostgresRepository) ListUserIDsDueBefore(ctx context.Context, t time.Time) ([]string, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT user_id FROM subscriptions
		WHERE status IN ('trial', 'active', 'past_due') AND current_period_end < $1
		ORDER BY current_period_end`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// Search lists subscriptions with their owners, newest first. users is
// read for display only (platform admin view).
func (r *PostgresRepository) Search(ctx context.Context, f usecase.SubscriptionFilter) (*usecase.SubscriptionPage, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	where := ` WHERE ($1 = '' OR lower(a.email) LIKE $1 OR lower(a.full_name) LIKE $1) AND ($2 = '' OR s.status::text = $2)`
	pattern := ""

	if query := strings.ToLower(strings.TrimSpace(f.Query)); query != "" {
		pattern = "%" + query + "%"
	}

	page := &usecase.SubscriptionPage{}

	err := q.QueryRow(ctx, `SELECT count(*)`+subscriptionFrom+` JOIN users a ON a.id = s.user_id`+where,
		pattern, string(f.Status)).Scan(&page.Total)
	if err != nil {
		return nil, err
	}

	rows, err := q.Query(ctx, `SELECT `+subscriptionColumns+`, COALESCE(a.email, ''), a.full_name`+subscriptionFrom+
		` JOIN users a ON a.id = s.user_id`+where+` ORDER BY s.created_at DESC LIMIT $3 OFFSET $4`,
		pattern, string(f.Status), f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			s       domain.Subscription
			plan    domain.Plan
			status  string
			summary usecase.SubscriptionSummary
		)

		dest, finish := subscriptionDest(&s, &plan, &status)
		if err := rows.Scan(append(dest, &summary.Email, &summary.FullName)...); err != nil {
			return nil, err
		}

		finish()

		s.Status, s.Plan = domain.SubscriptionStatus(status), &plan
		summary.Subscription = &s
		page.Items = append(page.Items, summary)
	}

	return page, rows.Err()
}

// Replace sets the user's feature switches to flags.
func (r *PostgresRepository) Replace(ctx context.Context, userID string, flags map[domain.Feature]bool) error {
	features := make([]string, 0, len(flags))
	enabled := make([]bool, 0, len(flags))

	for f, on := range flags {
		features = append(features, string(f))
		enabled = append(enabled, on)
	}

	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		INSERT INTO entitlements (user_id, feature, enabled, updated_at)
		SELECT $1, f::plan_feature, e, now() FROM unnest($2::text[], $3::boolean[]) AS t(f, e)
		ON CONFLICT (user_id, feature) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()`,
		userID, features, enabled)

	return err
}
