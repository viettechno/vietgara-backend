// Package usecase contains the application use cases of the License
// Management module: trial provisioning, plan changes and plan limits,
// the entitlement cache, plan administration and the subscription expiry
// job. See: FRD Section 3.3, LLD Section 3, Event Design Section 4.
package usecase

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Event types produced and consumed by this module (Event Design Section 4).
const (
	EventAccountEmailVerified      = "account.email_verified"
	EventEntitlementUpdated        = "entitlement.updated"
	EventSubscriptionExpiringSoon  = "subscription.expiring_soon"
	EventSubscriptionStatusChanged = "subscription.status_changed"
)

// keyAccountID is the account key of event and audit payloads.
const keyAccountID = "account_id"

// PlanRepository persists the plan catalogue. Finders return
// domain.ErrPlanNotFound.
type PlanRepository interface {
	// ListPublic returns the plans owners may choose, catalogue order.
	ListPublic(ctx context.Context) ([]*domain.Plan, error)
	// ListAll returns every plan (platform admins).
	ListAll(ctx context.Context) ([]*domain.Plan, error)
	FindByID(ctx context.Context, planID string) (*domain.Plan, error)
	FindByCode(ctx context.Context, code string) (*domain.Plan, error)
	// Create inserts p; a duplicate code yields domain.ErrPlanCodeTaken.
	Create(ctx context.Context, p *domain.Plan) error
	Update(ctx context.Context, p *domain.Plan) error
}

// SubscriptionPage is one page of the admin subscription list.
type SubscriptionPage struct {
	Items []SubscriptionSummary
	Total int
}

// SubscriptionSummary is a subscription with its owner's name and e-mail.
type SubscriptionSummary struct {
	Subscription *domain.Subscription
	Email        string
	FullName     string
}

// SubscriptionFilter narrows the admin subscription list.
type SubscriptionFilter struct {
	Query    string
	Status   domain.SubscriptionStatus
	Page     int
	PageSize int
}

// SubscriptionRepository persists subscriptions (one per owner account).
// Reads load the plan too; finders return domain.ErrSubscriptionNotFound.
type SubscriptionRepository interface {
	// CreateIfAbsent inserts s unless the account already has a
	// subscription, and reports whether it inserted.
	CreateIfAbsent(ctx context.Context, s *domain.Subscription) (bool, error)
	FindByAccountID(ctx context.Context, accountID string) (*domain.Subscription, error)
	// LockByAccountID is FindByAccountID holding a row lock until the
	// surrounding transaction ends.
	LockByAccountID(ctx context.Context, accountID string) (*domain.Subscription, error)
	// LockByID locks a subscription by its id.
	LockByID(ctx context.Context, id string) (*domain.Subscription, error)
	// Update persists the mutable fields of s.
	Update(ctx context.Context, s *domain.Subscription) error
	// ListAccountIDsDueBefore returns the accounts whose trial/active/past_due
	// subscription period ends before t.
	ListAccountIDsDueBefore(ctx context.Context, t time.Time) ([]string, error)
	// Search lists subscriptions for platform admins.
	Search(ctx context.Context, filter SubscriptionFilter) (*SubscriptionPage, error)
}

// EntitlementRepository persists the per-account feature switches.
type EntitlementRepository interface {
	Replace(ctx context.Context, accountID string, flags map[domain.Feature]bool) error
}

// EntitlementCache stores entitlements by owner account (LLD Section 3.3).
// Implementations: Redis (Memorystore) and in-memory.
type EntitlementCache interface {
	// Get reports a cache miss with (nil, nil).
	Get(ctx context.Context, accountID string) (*domain.Entitlements, error)
	Set(ctx context.Context, e *domain.Entitlements) error
	Delete(ctx context.Context, accountID string) error
}

// TransactionCounter counts business documents created per owner and day.
type TransactionCounter interface {
	// Increment adds one to the count of day and returns the new count.
	Increment(ctx context.Context, accountID, day string) (int, error)
	Get(ctx context.Context, accountID, day string) (int, error)
}

// UsageReader counts an owner's garages and staff across their garages
// (implemented by the Tenant module).
type UsageReader interface {
	OwnerUsage(ctx context.Context, ownerAccountID string) (garages, staff int, err error)
}

// GarageOwnerReader resolves the owner of a garage (Tenant module).
type GarageOwnerReader interface {
	GarageOwner(ctx context.Context, garageID string) (string, error)
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, accountID string, payload any) error
}

// AuditLogger records audited changes in the caller's transaction.
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// Option configures the use cases of this module.
type Option func(*options)

type options struct {
	now      func() time.Time
	grace    time.Duration
	location *time.Location
}

// WithClock replaces time.Now, e.g. to simulate time passing in tests.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// WithGracePeriod overrides domain.GracePeriod.
func WithGracePeriod(grace time.Duration) Option {
	return func(o *options) { o.grace = grace }
}

// businessLocation is where "a day" of the transaction limit is counted.
var businessLocation = mustLocation("Asia/Ho_Chi_Minh")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("ICT", 7*60*60)
	}

	return loc
}

func newOptions(opts []Option) options {
	o := options{now: time.Now, grace: domain.GracePeriod, location: businessLocation}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// today is the business day at now, "20060102".
func (o options) today() string {
	return o.now().In(o.location).Format("20060102")
}

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	var invalid domain.InvalidPlanError

	switch {
	case isAppError(err):
		return err
	case errors.Is(err, domain.ErrSubscriptionNotFound), errors.Is(err, domain.ErrPlanNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
	case errors.Is(err, domain.ErrPlanNotAvailable):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonPlanNotAvailable)
	case errors.Is(err, domain.ErrPlanCodeTaken):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason(domain.ReasonPlanCodeTaken).WithField("code", apperror.IssueTaken)
	case errors.As(err, &invalid):
		return apperror.Validation(string(invalid), apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrInvalidExtension):
		return apperror.Validation("days", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrSubscriptionCancelled):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err)
	default:
		return apperror.Internal(op, err)
	}
}

func isAppError(err error) bool {
	_, ok := apperror.As(err)

	return ok
}

// limitReached builds the PLAN_LIMIT_REACHED error of limit.
func limitReached(limit domain.LimitType, allowed int) error {
	return apperror.New(apperror.CodeFailedPrecondition, "your plan allows at most "+strconv.Itoa(allowed)+" for "+string(limit)+"; upgrade to add more").
		WithReason(domain.ReasonPlanLimitReached).WithField(string(limit), "limit_reached")
}
