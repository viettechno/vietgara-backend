// Package licensetest provides in-memory fakes of the License module's
// dependencies for tests of the use cases and handlers.
package licensetest

import (
	"context"
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
	mu       sync.Mutex
	plans    map[string]*domain.Plan
	subs     map[string]*domain.Subscription // by user
	changes  map[string]*domain.PlanChange   // pending change by subscription id
	Flags    map[string]map[domain.Feature]bool
	Emails   map[string]string // user -> e-mail for Search
	Fail     map[string]error
	Garages  map[string]int // user -> garages in use
	Staff    map[string]int
	Owners   map[string]string // garage -> owner
	Events   []string
	Audits   []string
	AuditErr error
}

// NewStore returns a store with the seeded catalogue: trial, starter,
// pro (2 garages, 20 staff, 100/day) and a hidden legacy plan.
func NewStore() *Store {
	s := &Store{
		plans: map[string]*domain.Plan{}, subs: map[string]*domain.Subscription{}, changes: map[string]*domain.PlanChange{}, Flags: map[string]map[domain.Feature]bool{},
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

	return s.withPlan(sub), nil
}

// withPlan returns a copy of sub with its plan and pending change loaded.
func (s *Store) withPlan(sub *domain.Subscription) *domain.Subscription {
	copied := *sub
	plan := *s.plans[sub.PlanID]
	copied.Plan = &plan
	copied.PendingChange = nil

	if c, ok := s.changes[sub.ID]; ok {
		change := *c
		target := *s.plans[c.PlanID]
		change.Plan = &target
		copied.PendingChange = &change
	}

	return &copied
}

// PendingChangeOf returns a copy of the user's pending plan change, or nil.
func (s *Store) PendingChangeOf(userID string) *domain.PlanChange {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, ok := s.subs[userID]
	if !ok {
		return nil
	}

	return s.withPlan(sub).PendingChange
}

// CreateChange implements usecase.SubscriptionRepository.
func (s *Store) CreateChange(_ context.Context, c *domain.PlanChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.change.create"); err != nil {
		return err
	}

	c.ID = uuid.NewString()
	copied := *c
	s.changes[c.SubscriptionID] = &copied

	return nil
}

// SaveChange implements usecase.SubscriptionRepository.
func (s *Store) SaveChange(_ context.Context, c *domain.PlanChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("subs.change.save"); err != nil {
		return err
	}

	current, ok := s.changes[c.SubscriptionID]
	if !ok || current.ID != c.ID {
		return domain.ErrNoPendingPlanChange
	}

	if c.Status.Pending() {
		copied := *c
		s.changes[c.SubscriptionID] = &copied
	} else {
		delete(s.changes, c.SubscriptionID)
	}

	return nil
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
		change, pending := s.changes[sub.ID]

		if (live && sub.CurrentPeriodEnd.Before(t)) || (pending && change.EffectiveAt.Before(t)) {
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
			page.Items = append(page.Items, usecase.SubscriptionSummary{Subscription: s.withPlan(sub), Email: s.Emails[user]})
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
	e.Entitlements = usecase.NewEntitlementUseCase(e.Store, e.Cache, e.Store, e.Counter, e.Subscriptions, clock)
	e.Expiry = usecase.NewExpiryJob(e.Store, e.Store, e.Store, e.Cache, e.Store, e.Store, clock, usecase.WithGracePeriod(domain.GracePeriod))

	return e
}
