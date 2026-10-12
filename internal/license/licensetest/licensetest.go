// Package licensetest provides in-memory fakes of the License module's
// dependencies for tests of the use cases and handlers.
package licensetest

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/repository"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Store fakes plans, subscriptions and entitlements.
type Store struct {
	mu         sync.Mutex
	plans      map[string]*domain.Plan
	subs       map[string]*domain.Subscription // by user
	requests   []*domain.PlanPaymentRequest
	ledger     []*domain.CreditEntry
	settings   domain.PaymentSettings
	files      map[string]string // file id -> content type
	lastRemind time.Time
	Flags      map[string]map[domain.Feature]bool
	Emails     map[string]string // user -> e-mail for Search
	Fail       map[string]error
	Garages    map[string]int // user -> garages in use
	Staff      map[string]int
	Owners     map[string]string // garage -> owner
	Events     []string
	Audits     []string
	AuditErr   error
}

// NewStore returns a store with the seeded catalogue: trial, starter,
// pro (2 garages, 20 staff, 100/day) and a hidden legacy plan.
func NewStore() *Store {
	s := &Store{
		plans: map[string]*domain.Plan{}, subs: map[string]*domain.Subscription{}, files: map[string]string{}, Flags: map[string]map[domain.Feature]bool{},
		Emails: map[string]string{}, Fail: map[string]error{}, Garages: map[string]int{}, Staff: map[string]int{}, Owners: map[string]string{},
	}

	for _, p := range []*domain.Plan{
		{ID: "p-trial", Code: domain.TrialPlanCode, Name: "Trial", Currency: "VND", BillingCycle: domain.BillingCycleMonthly, MaxGarages: 1, MaxStaff: 5, MaxTransactionsPerDay: 2, Features: domain.AllFeatures, Active: true},
		{ID: "p-starter", Code: "starter", Name: "Starter", Price: 299000, Currency: "VND", BillingCycle: domain.BillingCycleMonthly, MaxGarages: 1, MaxStaff: 5, MaxTransactionsPerDay: 50, Features: []domain.Feature{domain.FeatureCustomers}, Public: true, Active: true, SortOrder: 10},
		{ID: "p-pro", Code: "pro", Name: "Pro", Price: 599000, Currency: "VND", BillingCycle: domain.BillingCycleYearly, MaxGarages: 2, MaxStaff: 20, MaxTransactionsPerDay: 100, Features: domain.AllFeatures, Public: true, Active: true, SortOrder: 20},
		{ID: "p-legacy", Code: "legacy", Name: "Legacy", Currency: "VND", BillingCycle: domain.BillingCycleMonthly, MaxGarages: 1, MaxStaff: 1, MaxTransactionsPerDay: 1, Active: false},
	} {
		s.plans[p.ID] = p
	}

	return s
}

func (s *Store) fail(op string) error { return s.Fail[op] }

// Plans returns the plan repository view.
func (s *Store) Plans() Plans { return Plans{s} }

// Plans implements usecase.PlanRepository.
type Plans struct{ s *Store }

func (p Plans) list(match func(*domain.Plan) bool) ([]*domain.Plan, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if err := p.s.fail("plans.list"); err != nil {
		return nil, err
	}

	var out []*domain.Plan

	for _, plan := range p.s.plans {
		if match(plan) {
			copied := *plan
			out = append(out, &copied)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })

	return out, nil
}

// ListPublic implements usecase.PlanRepository.
func (p Plans) ListPublic(context.Context) ([]*domain.Plan, error) {
	return p.list(func(plan *domain.Plan) bool { return plan.Selectable() })
}

// ListAll implements usecase.PlanRepository.
func (p Plans) ListAll(context.Context) ([]*domain.Plan, error) {
	return p.list(func(*domain.Plan) bool { return true })
}

// FindByID implements usecase.PlanRepository.
func (p Plans) FindByID(_ context.Context, id string) (*domain.Plan, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	plan, ok := p.s.plans[id]
	if !ok {
		return nil, domain.ErrPlanNotFound
	}

	copied := *plan

	return &copied, nil
}

// FindByCode implements usecase.PlanRepository.
func (p Plans) FindByCode(_ context.Context, code string) (*domain.Plan, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if err := p.s.fail("plans.code"); err != nil {
		return nil, err
	}

	for _, plan := range p.s.plans {
		if plan.Code == code {
			copied := *plan

			return &copied, nil
		}
	}

	return nil, domain.ErrPlanNotFound
}

// Create implements usecase.PlanRepository.
func (p Plans) Create(_ context.Context, plan *domain.Plan) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	for _, other := range p.s.plans {
		if other.Code == plan.Code {
			return domain.ErrPlanCodeTaken
		}
	}

	plan.ID = uuid.NewString()
	copied := *plan
	p.s.plans[plan.ID] = &copied

	return nil
}

// Update implements usecase.PlanRepository.
func (p Plans) Update(_ context.Context, plan *domain.Plan) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if err := p.s.fail("plans.update"); err != nil {
		return err
	}

	copied := *plan
	p.s.plans[plan.ID] = &copied

	return nil
}

// CreateIfAbsent implements usecase.SubscriptionRepository.
func (s *Store) CreateIfAbsent(_ context.Context, sub *domain.Subscription) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.create"); err != nil {
		return false, err
	}

	if _, ok := s.subs[sub.UserID]; ok {
		return false, nil
	}

	sub.ID = uuid.NewString()
	copied := *sub
	s.subs[sub.UserID] = &copied

	return true, nil
}

// FindByUserID implements usecase.SubscriptionRepository.
func (s *Store) FindByUserID(_ context.Context, userID string) (*domain.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.find"); err != nil {
		return nil, err
	}

	sub, ok := s.subs[userID]
	if !ok {
		return nil, domain.ErrSubscriptionNotFound
	}

	copied := *sub
	plan := *s.plans[sub.PlanID]
	copied.Plan = &plan

	return &copied, nil
}

// LockByUserID implements usecase.SubscriptionRepository.
func (s *Store) LockByUserID(ctx context.Context, userID string) (*domain.Subscription, error) {
	if err := s.fail("subs.lock"); err != nil {
		return nil, err
	}

	return s.FindByUserID(ctx, userID)
}

// LockByID implements usecase.SubscriptionRepository.
func (s *Store) LockByID(ctx context.Context, id string) (*domain.Subscription, error) {
	s.mu.Lock()

	var userID string

	for user, sub := range s.subs {
		if sub.ID == id {
			userID = user
		}
	}

	s.mu.Unlock()

	if userID == "" {
		return nil, domain.ErrSubscriptionNotFound
	}

	return s.FindByUserID(ctx, userID)
}

// Update implements usecase.SubscriptionRepository.
func (s *Store) Update(_ context.Context, sub *domain.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.update"); err != nil {
		return err
	}

	copied := *sub
	s.subs[sub.UserID] = &copied

	return nil
}

// ListUserIDsDueBefore implements usecase.SubscriptionRepository.
func (s *Store) ListUserIDsDueBefore(_ context.Context, t time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.due"); err != nil {
		return nil, err
	}

	var ids []string

	for user, sub := range s.subs {
		live := sub.Status == domain.StatusTrial || sub.Status == domain.StatusActive || sub.Status == domain.StatusPastDue
		if live && sub.CurrentPeriodEnd.Before(t) {
			ids = append(ids, user)
		}
	}

	sort.Strings(ids)

	return ids, nil
}

// Search implements usecase.SubscriptionRepository.
func (s *Store) Search(_ context.Context, f usecase.SubscriptionFilter) (*usecase.SubscriptionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.search"); err != nil {
		return nil, err
	}

	page := &usecase.SubscriptionPage{}

	for user, sub := range s.subs {
		if (f.Query == "" || strings.Contains(s.Emails[user], f.Query)) && (f.Status == "" || sub.Status == f.Status) {
			copied := *sub
			plan := *s.plans[sub.PlanID]
			copied.Plan = &plan
			page.Items = append(page.Items, usecase.SubscriptionSummary{Subscription: &copied, Email: s.Emails[user]})
		}
	}

	page.Total = len(page.Items)

	return page, nil
}

// Replace implements usecase.EntitlementRepository.
func (s *Store) Replace(_ context.Context, userID string, flags map[domain.Feature]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("entitlements"); err != nil {
		return err
	}

	s.Flags[userID] = flags

	return nil
}

// OwnerUsage implements usecase.UsageReader.
func (s *Store) OwnerUsage(_ context.Context, owner string) (int, int, error) {
	if err := s.fail("usage"); err != nil {
		return 0, 0, err
	}

	return s.Garages[owner], s.Staff[owner], nil
}

// GarageOwner implements usecase.GarageOwnerReader.
func (s *Store) GarageOwner(_ context.Context, garageID string) (string, error) {
	owner, ok := s.Owners[garageID]
	if !ok {
		return "", domain.ErrSubscriptionNotFound
	}

	return owner, nil
}

// Publish implements usecase.EventPublisher.
func (s *Store) Publish(_ context.Context, eventType, _, _ string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("events"); err != nil {
		return err
	}

	s.Events = append(s.Events, eventType)

	return nil
}

// Record implements usecase.AuditLogger.
func (s *Store) Record(_ context.Context, e audit.Entry) error {
	s.Audits = append(s.Audits, e.Action)

	return s.AuditErr
}

// WithTransaction runs fn directly (usecase.Transactor).
func (s *Store) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	if err := s.fail("tx"); err != nil {
		return err
	}

	return fn(ctx)
}

// Env wires the license use cases to fakes.
type Env struct {
	Store         *Store
	Cache         *repository.MemoryEntitlementCache
	Counter       *repository.MemoryTransactionCounter
	Subscriptions *usecase.SubscriptionUseCase
	Entitlements  *usecase.EntitlementUseCase
	Payments      *usecase.PaymentUseCase
	Expiry        *usecase.ExpiryJob
	Now           time.Time
}

// NewEnv returns use cases whose clock reads e.Now.
func NewEnv() *Env {
	e := &Env{
		Store: NewStore(), Cache: repository.NewMemoryEntitlementCache(time.Hour), Counter: repository.NewMemoryTransactionCounter(),
		Now: time.Now().UTC(),
	}
	clock := usecase.WithClock(func() time.Time { return e.Now })

	e.Subscriptions = usecase.NewSubscriptionUseCase(usecase.SubscriptionDeps{
		Plans: e.Store.Plans(), Subscriptions: e.Store, Entitlements: e.Store, Usage: e.Store, Counter: e.Counter,
		Events: e.Store, Cache: e.Cache, Audit: e.Store,
	}, clock)
	e.Payments = usecase.NewPaymentUseCase(usecase.PaymentDeps{
		Plans: e.Store.Plans(), Subscriptions: e.Store, Entitlements: e.Store, Payments: e.Store, Files: e.Store, Usage: e.Store,
		Events: e.Store, Cache: e.Cache, Audit: e.Store,
	}, clock)
	e.Entitlements = usecase.NewEntitlementUseCase(e.Store, e.Cache, e.Store, e.Counter, e.Subscriptions, clock)
	e.Expiry = usecase.NewExpiryJob(e.Store, e.Store, e.Store, e.Cache, e.Store, e.Store, clock, usecase.WithGracePeriod(domain.GracePeriod))

	return e
}

// CreateRequest implements usecase.PaymentRepository: one open request per
// owner and unique references.
func (s *Store) CreateRequest(_ context.Context, r *domain.PlanPaymentRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.create"); err != nil {
		return err
	}

	for _, e := range s.requests {
		if e.Reference == r.Reference {
			return domain.ErrReferenceTaken
		}

		if e.UserID == r.UserID && e.Status.Open() && r.Status.Open() {
			return domain.ErrRequestAlreadyOpen
		}
	}

	r.ID = uuid.NewString()
	r.CreatedAt = time.Now().UTC()
	copied := *r
	s.requests = append(s.requests, &copied)

	return nil
}

func (s *Store) requestCopy(r *domain.PlanPaymentRequest) *domain.PlanPaymentRequest {
	copied := *r
	if plan, ok := s.plans[r.PlanID]; ok {
		p := *plan
		copied.Plan = &p
	}

	return &copied
}

// FindRequest implements usecase.PaymentRepository.
func (s *Store) FindRequest(_ context.Context, id string) (*domain.PlanPaymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.find"); err != nil {
		return nil, err
	}

	for _, r := range s.requests {
		if r.ID == id {
			return s.requestCopy(r), nil
		}
	}

	return nil, domain.ErrPaymentRequestNotFound
}

// LockRequest implements usecase.PaymentRepository.
func (s *Store) LockRequest(ctx context.Context, id string) (*domain.PlanPaymentRequest, error) {
	if err := s.fail("payments.lock"); err != nil {
		return nil, err
	}

	return s.FindRequest(ctx, id)
}

// FindOpenRequest implements usecase.PaymentRepository.
func (s *Store) FindOpenRequest(_ context.Context, userID string) (*domain.PlanPaymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.open"); err != nil {
		return nil, err
	}

	for _, r := range s.requests {
		if r.UserID == userID && r.Status.Open() {
			return s.requestCopy(r), nil
		}
	}

	return nil, domain.ErrPaymentRequestNotFound
}

// SaveRequest implements usecase.PaymentRepository.
func (s *Store) SaveRequest(_ context.Context, r *domain.PlanPaymentRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.save"); err != nil {
		return err
	}

	for i, e := range s.requests {
		if e.ID == r.ID {
			copied := *r
			s.requests[i] = &copied

			return nil
		}
	}

	return domain.ErrPaymentRequestNotFound
}

// ListRequests implements usecase.PaymentRepository.
func (s *Store) ListRequests(_ context.Context, userID string, status domain.PaymentStatus) ([]*domain.PlanPaymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.list"); err != nil {
		return nil, err
	}

	var out []*domain.PlanPaymentRequest

	for _, r := range slices.Backward(s.requests) {
		if r.UserID == userID && (status == "" || r.Status == status) {
			out = append(out, s.requestCopy(r))
		}
	}

	return out, nil
}

// OpenRequests implements usecase.PaymentRepository.
func (s *Store) OpenRequests(_ context.Context, subscriptionIDs []string) (map[string]*domain.PlanPaymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.openrequests"); err != nil {
		return nil, err
	}

	out := map[string]*domain.PlanPaymentRequest{}

	for _, r := range s.requests {
		if r.Status.Open() && slices.Contains(subscriptionIDs, r.SubscriptionID) {
			out[r.SubscriptionID] = s.requestCopy(r)
		}
	}

	return out, nil
}

// SearchRequests implements usecase.PaymentRepository.
func (s *Store) SearchRequests(_ context.Context, f usecase.ReviewFilter) (*usecase.ReviewPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.search"); err != nil {
		return nil, err
	}

	page := &usecase.ReviewPage{}

	for _, r := range s.requests {
		if r.Status != f.Status {
			continue
		}

		email := s.Emails[r.UserID]
		if f.Query != "" && !strings.Contains(email, f.Query) && !strings.Contains(strings.ToLower(r.Reference), strings.ToLower(f.Query)) {
			continue
		}

		page.Items = append(page.Items, usecase.ReviewItem{Request: s.requestCopy(r), Email: email})
	}

	page.Total = len(page.Items)

	return page, nil
}

// WaitingForReview implements usecase.PaymentRepository.
func (s *Store) WaitingForReview(context.Context) (int, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.waiting"); err != nil {
		return 0, time.Time{}, err
	}

	var (
		count  int
		oldest time.Time
	)

	for _, r := range s.requests {
		if r.Status == domain.PaymentSubmitted {
			count++

			if r.SubmittedAt != nil && (oldest.IsZero() || r.SubmittedAt.Before(oldest)) {
				oldest = *r.SubmittedAt
			}
		}
	}

	return count, oldest, nil
}

// AddCreditEntry implements usecase.PaymentRepository.
func (s *Store) AddCreditEntry(_ context.Context, e *domain.CreditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.credit"); err != nil {
		return err
	}

	e.ID = uuid.NewString()
	e.CreatedAt = time.Now().UTC()
	copied := *e
	s.ledger = append(s.ledger, &copied)

	return nil
}

// ListCredit implements usecase.PaymentRepository.
func (s *Store) ListCredit(_ context.Context, userID string) ([]*domain.CreditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.ledger"); err != nil {
		return nil, err
	}

	var out []*domain.CreditEntry

	for _, e := range slices.Backward(s.ledger) {
		if e.UserID == userID {
			copied := *e
			out = append(out, &copied)
		}
	}

	return out, nil
}

// Ledger returns the credit entries recorded so far, oldest first.
func (s *Store) Ledger() []*domain.CreditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.ledger)
}

// ClaimReviewReminder implements usecase.PaymentRepository.
func (s *Store) ClaimReviewReminder(_ context.Context, now time.Time, minInterval time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.remind"); err != nil {
		return false, err
	}

	if !s.lastRemind.IsZero() && s.lastRemind.After(now.Add(-minInterval)) {
		return false, nil
	}

	s.lastRemind = now

	return true, nil
}

// Settings implements usecase.PaymentRepository.
func (s *Store) Settings(context.Context) (*domain.PaymentSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.settings"); err != nil {
		return nil, err
	}

	copied := s.settings

	return &copied, nil
}

// SaveSettings implements usecase.PaymentRepository.
func (s *Store) SaveSettings(_ context.Context, settings *domain.PaymentSettings, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("payments.savesettings"); err != nil {
		return err
	}

	s.settings = *settings

	return nil
}

// StoreEvidence implements usecase.FileStore.
func (s *Store) StoreEvidence(_ context.Context, _ string, content []byte, contentType string) (string, error) {
	return s.storeFile("evidence", content, contentType, domain.ErrInvalidEvidence)
}

// StoreQR implements usecase.FileStore.
func (s *Store) StoreQR(_ context.Context, _ string, content []byte, contentType string) (string, error) {
	return s.storeFile("qr", content, contentType, domain.ErrInvalidQR)
}

func (s *Store) storeFile(kind string, content []byte, contentType string, invalid error) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("files.store"); err != nil {
		return "", err
	}

	if len(content) == 0 || contentType == "text/plain" {
		return "", invalid
	}

	id := kind + "-" + uuid.NewString()
	s.files[id] = contentType

	return id, nil
}

// SignedURL implements usecase.FileStore.
func (s *Store) SignedURL(_ context.Context, fileID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.files[fileID]; !ok {
		return "", domain.ErrPaymentRequestNotFound
	}

	return "https://files.example/" + fileID + "?sig=1", nil
}
