// Package tenanttest provides in-memory fakes of the Tenant module's
// dependencies for tests of the use cases and handlers.
package tenanttest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Store is an in-memory implementation of every Tenant repository plus
// access.Reader and UsageReader; failures can be injected per operation.
type Store struct {
	mu          sync.Mutex
	garages     map[string]*domain.Garage
	staff       map[string]*domain.Staff
	roles       map[string]*domain.Role
	groups      map[string]*domain.StaffGroup
	members     map[string][]string // staff id -> group ids
	invitations map[string]*domain.Invitation
	Fail        map[string]error
}

func NewStore() *Store {
	return &Store{
		garages: map[string]*domain.Garage{}, staff: map[string]*domain.Staff{}, roles: map[string]*domain.Role{},
		groups: map[string]*domain.StaffGroup{}, members: map[string][]string{}, invitations: map[string]*domain.Invitation{},
		Fail: map[string]error{},
	}
}

func (s *Store) err(op string) error { return s.Fail[op] }

// Garages

func (s *Store) Create(_ context.Context, g *domain.Garage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("garage.create"); err != nil {
		return err
	}

	copied := *g
	s.garages[g.ID] = &copied

	return nil
}

func (s *Store) FindByID(_ context.Context, id string) (*domain.Garage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("garage.find"); err != nil {
		return nil, err
	}

	g, ok := s.garages[id]
	if !ok {
		return nil, domain.ErrGarageNotFound
	}

	copied := *g

	return &copied, nil
}

func (s *Store) ListForAccount(_ context.Context, accountID string) ([]*domain.Garage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("garage.list"); err != nil {
		return nil, err
	}

	var out []*domain.Garage

	for _, g := range s.garages {
		if g.OwnerAccountID == accountID {
			out = append(out, g)
		}
	}

	return out, nil
}

func (s *Store) Update(_ context.Context, g *domain.Garage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("garage.update"); err != nil {
		return err
	}

	copied := *g
	s.garages[g.ID] = &copied

	return nil
}

// Access & usage

func (s *Store) GarageAccess(_ context.Context, garageID, accountID string) (*access.Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("access"); err != nil {
		return nil, err
	}

	g := s.garages[garageID]
	if g == nil {
		return nil, access.ErrNoMembership
	}

	return &access.Access{
		GarageID: garageID, AccountID: accountID, OwnerAccountID: g.OwnerAccountID,
		Owner: g.OwnerAccountID == accountID, Active: g.Status == domain.GarageStatusActive,
	}, nil
}

func (s *Store) OwnerUsage(_ context.Context, owner string) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.err("usage"); err != nil {
		return 0, 0, err
	}

	garages, staff := 0, map[string]bool{}

	for _, g := range s.garages {
		if g.OwnerAccountID == owner && g.Status == domain.GarageStatusActive {
			garages++
		}
	}

	for _, m := range s.staff {
		if g := s.garages[m.GarageID]; g != nil && g.OwnerAccountID == owner && m.AccountID != owner && m.Status == domain.StaffStatusActive {
			staff[m.AccountID] = true
		}
	}

	for _, i := range s.invitations {
		if g := s.garages[i.GarageID]; g != nil && g.OwnerAccountID == owner && i.Status == domain.InvitationPending {
			staff[i.AccountID] = true
		}
	}

	return garages, len(staff), nil
}

// StaffRepo, RoleRepo, GroupRepo and InvitationRepo adapt Store to the
// repository interfaces whose method names collide.
type StaffRepo struct{ *Store }

func (r StaffRepo) Create(_ context.Context, m *domain.Staff) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.create"); err != nil {
		return err
	}

	for _, existing := range r.staff {
		if existing.GarageID == m.GarageID && existing.AccountID == m.AccountID {
			return domain.ErrAlreadyMember
		}
	}

	m.ID = uuid.NewString()
	copied := *m
	r.staff[m.ID] = &copied

	return nil
}

func (r StaffRepo) load(m *domain.Staff) *domain.Staff {
	copied := *m
	copied.GroupIDs = append([]string{}, r.members[m.ID]...)
	copied.Owner = r.garages[m.GarageID] != nil && r.garages[m.GarageID].OwnerAccountID == m.AccountID
	copied.Permissions = nil

	for _, gid := range copied.GroupIDs {
		if g := r.groups[gid]; g != nil && r.roles[g.RoleID] != nil {
			copied.Permissions = append(copied.Permissions, r.roles[g.RoleID].Permissions...)
		}
	}

	copied.Permissions = access.Normalize(copied.Permissions)

	return &copied
}

func (r StaffRepo) FindByID(_ context.Context, garageID, staffID string) (*domain.Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.find"); err != nil {
		return nil, err
	}

	m, ok := r.staff[staffID]
	if !ok || m.GarageID != garageID {
		return nil, domain.ErrStaffNotFound
	}

	return r.load(m), nil
}

func (r StaffRepo) FindByAccount(_ context.Context, garageID, accountID string) (*domain.Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.findByAccount"); err != nil {
		return nil, err
	}

	for _, m := range r.staff {
		if m.GarageID == garageID && m.AccountID == accountID {
			return r.load(m), nil
		}
	}

	return nil, domain.ErrStaffNotFound
}

func (r StaffRepo) List(_ context.Context, garageID string) ([]*domain.Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.list"); err != nil {
		return nil, err
	}

	var out []*domain.Staff

	for _, m := range r.staff {
		if m.GarageID == garageID {
			out = append(out, r.load(m))
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Owner && !out[j].Owner })

	return out, nil
}

func (r StaffRepo) UpdateStatus(_ context.Context, m *domain.Staff) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.update"); err != nil {
		return err
	}

	r.staff[m.ID].Status = m.Status

	return nil
}

func (r StaffRepo) SetGroups(_ context.Context, _, staffID string, groupIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.groups"); err != nil {
		return err
	}

	r.members[staffID] = groupIDs

	return nil
}

func (r StaffRepo) Delete(_ context.Context, _, staffID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("staff.delete"); err != nil {
		return err
	}

	delete(r.staff, staffID)

	return nil
}

type RoleRepo struct{ *Store }

func (r RoleRepo) ListByOwner(_ context.Context, owner string) ([]*domain.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("role.list"); err != nil {
		return nil, err
	}

	var out []*domain.Role

	for _, role := range r.roles {
		if role.OwnerAccountID == owner {
			copied := *role
			out = append(out, &copied)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

func (r RoleRepo) FindByID(_ context.Context, id string) (*domain.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, ok := r.roles[id]
	if !ok {
		return nil, domain.ErrRoleNotFound
	}

	copied := *role

	return &copied, nil
}

func (r RoleRepo) nameTaken(role *domain.Role) bool {
	for _, other := range r.roles {
		if other.ID != role.ID && other.OwnerAccountID == role.OwnerAccountID && strings.EqualFold(other.Name, role.Name) {
			return true
		}
	}

	return false
}

func (r RoleRepo) Create(_ context.Context, role *domain.Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nameTaken(role) {
		return domain.ErrRoleNameTaken
	}

	role.ID = uuid.NewString()
	copied := *role
	r.roles[role.ID] = &copied

	return nil
}

func (r RoleRepo) CreateDefault(_ context.Context, role *domain.Role) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("role.default"); err != nil {
		return false, err
	}

	for _, other := range r.roles {
		if other.OwnerAccountID == role.OwnerAccountID && other.DefaultKey != nil && *other.DefaultKey == *role.DefaultKey {
			return false, nil
		}
	}

	role.ID = uuid.NewString()
	copied := *role
	r.roles[role.ID] = &copied

	return true, nil
}

func (r RoleRepo) Update(_ context.Context, role *domain.Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nameTaken(role) {
		return domain.ErrRoleNameTaken
	}

	copied := *role
	r.roles[role.ID] = &copied

	return nil
}

func (r RoleRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, g := range r.groups {
		if g.RoleID == id {
			return domain.ErrRoleInUse
		}
	}

	delete(r.roles, id)

	return nil
}

type GroupRepo struct{ *Store }

func (r GroupRepo) List(_ context.Context, garageID string) ([]*domain.StaffGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("group.list"); err != nil {
		return nil, err
	}

	var out []*domain.StaffGroup

	for _, g := range r.groups {
		if g.GarageID == garageID {
			copied := *g
			out = append(out, &copied)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

func (r GroupRepo) FindByID(_ context.Context, garageID, id string) (*domain.StaffGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	g, ok := r.groups[id]
	if !ok || g.GarageID != garageID {
		return nil, domain.ErrGroupNotFound
	}

	copied := *g

	return &copied, nil
}

func (r GroupRepo) Create(_ context.Context, g *domain.StaffGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, other := range r.groups {
		if other.GarageID == g.GarageID && strings.EqualFold(other.Name, g.Name) {
			return domain.ErrGroupNameTaken
		}
	}

	g.ID = uuid.NewString()
	copied := *g
	r.groups[g.ID] = &copied

	return nil
}

func (r GroupRepo) CreateDefault(ctx context.Context, g *domain.StaffGroup) (bool, error) {
	return true, r.Create(ctx, g)
}

func (r GroupRepo) Update(_ context.Context, g *domain.StaffGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	copied := *g
	r.groups[g.ID] = &copied

	return nil
}

func (r GroupRepo) Delete(_ context.Context, _, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("group.delete"); err != nil {
		return err
	}

	delete(r.groups, id)

	return nil
}

type InvitationRepo struct{ *Store }

func (r InvitationRepo) Create(_ context.Context, inv *domain.Invitation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, other := range r.invitations {
		if other.GarageID == inv.GarageID && other.AccountID == inv.AccountID && other.Status == domain.InvitationPending {
			return domain.ErrAlreadyInvited
		}
	}

	inv.ID = uuid.NewString()
	copied := *inv
	copied.GarageName = r.garages[inv.GarageID].Name
	r.invitations[inv.ID] = &copied

	return nil
}

func (r InvitationRepo) FindByID(_ context.Context, id string) (*domain.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	inv, ok := r.invitations[id]
	if !ok {
		return nil, domain.ErrInvitationNotFound
	}

	copied := *inv

	return &copied, nil
}

func (r InvitationRepo) list(match func(*domain.Invitation) bool) ([]*domain.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("invitation.list"); err != nil {
		return nil, err
	}

	var out []*domain.Invitation

	for _, inv := range r.invitations {
		if match(inv) {
			copied := *inv
			out = append(out, &copied)
		}
	}

	return out, nil
}

func (r InvitationRepo) ListByGarage(_ context.Context, garageID string) ([]*domain.Invitation, error) {
	return r.list(func(i *domain.Invitation) bool { return i.GarageID == garageID })
}

func (r InvitationRepo) ListByAccount(_ context.Context, accountID string) ([]*domain.Invitation, error) {
	return r.list(func(i *domain.Invitation) bool { return i.AccountID == accountID })
}

func (r InvitationRepo) Update(_ context.Context, inv *domain.Invitation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.err("invitation.update"); err != nil {
		return err
	}

	copied := *inv
	r.invitations[inv.ID] = &copied

	return nil
}

func (r InvitationRepo) ExpireStale(context.Context, string, string) error {
	return r.err("invitation.expire")
}

// Directory is a fake AccountDirectory.
type Directory struct {
	Accounts map[string]*usecase.AccountSummary
	Err      error
}

func (d *Directory) FindByEmail(_ context.Context, email string) (*usecase.AccountSummary, error) {
	for _, a := range d.Accounts {
		if strings.EqualFold(a.Email, email) {
			return a, nil
		}
	}

	return nil, domain.ErrAccountNotFound
}

func (d *Directory) FindByPhone(_ context.Context, phone string) (*usecase.AccountSummary, error) {
	for _, a := range d.Accounts {
		if a.Phone != nil && *a.Phone == phone {
			return a, nil
		}
	}

	return nil, domain.ErrAccountNotFound
}

func (d *Directory) FindByID(_ context.Context, id string) (*usecase.AccountSummary, error) {
	if a, ok := d.Accounts[id]; ok {
		return a, nil
	}

	return nil, domain.ErrAccountNotFound
}

func (d *Directory) ListByIDs(_ context.Context, ids []string) (map[string]*usecase.AccountSummary, error) {
	if d.Err != nil {
		return nil, d.Err
	}

	out := map[string]*usecase.AccountSummary{}

	for _, id := range ids {
		if a, ok := d.Accounts[id]; ok {
			out[id] = a
		}
	}

	return out, nil
}

// Limits enforces fixed maximums.
type Limits struct{ Garages, Staff int }

func (l Limits) CheckLimit(_ context.Context, _ string, limit usecase.Limit, current int) error {
	allowed := l.Garages
	if limit == usecase.LimitStaff {
		allowed = l.Staff
	}

	if current >= allowed {
		return apperror.New(apperror.CodeFailedPrecondition, "limit").WithReason("PLAN_LIMIT_REACHED")
	}

	return nil
}

type Logos struct {
	mu      sync.Mutex
	Deleted []string
	Err     error
}

func (l *Logos) Put(_ context.Context, key, _ string, _ []byte) (string, error) {
	return "/files/" + key, l.Err
}

func (l *Logos) Delete(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.Deleted = append(l.Deleted, key)

	return nil
}

func (l *Logos) KeyFromURL(url string) (string, bool) {
	return strings.CutPrefix(url, "/files/")
}

type Mailer struct {
	mu   sync.Mutex
	Sent []mail.Message
}

func (m *Mailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Sent = append(m.Sent, msg)

	return nil
}

type Events struct{ Types []string }

func (e *Events) Publish(_ context.Context, t, _, _ string, _ any) error {
	e.Types = append(e.Types, t)

	return nil
}

type AuditLog struct {
	Actions []string
	Err     error
}

func (a *AuditLog) Record(_ context.Context, e audit.Entry) error {
	a.Actions = append(a.Actions, e.Action)

	return a.Err
}

type PassTx struct{}

func (PassTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

// Accounts of the fixture directory.
const (
	Owner = "owner-1"
	Staff = "staff-1"
)

// Env is a TenantUseCase wired to fakes.
type Env struct {
	UC     *usecase.TenantUseCase
	Store  *Store
	Dir    *Directory
	Mail   *Mailer
	Events *Events
	Audit  *AuditLog
	Logos  *Logos
	Now    time.Time
}

// NewEnv wires a use case whose directory knows Owner (English), Staff
// (Vietnamese, phone 0901234567) and an inactive account "off".
func NewEnv(lim Limits) *Env {
	phone := "0901234567"
	e := &Env{
		Store: NewStore(), Mail: &Mailer{}, Events: &Events{}, Audit: &AuditLog{}, Logos: &Logos{},
		Now: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC),
		Dir: &Directory{Accounts: map[string]*usecase.AccountSummary{
			Owner: {ID: Owner, FullName: "Minh", Email: "owner@example.com", Locale: "en", Active: true, EmailVerified: true},
			Staff: {ID: Staff, FullName: "Lan", Email: "lan@example.com", Phone: &phone, Locale: "vi", Active: true},
			"off": {ID: "off", FullName: "Off", Email: "off@example.com", Active: false},
		}},
	}
	e.UC = usecase.NewTenantUseCase(usecase.Deps{
		Garages: e.Store, Staff: StaffRepo{e.Store}, Roles: RoleRepo{e.Store}, Groups: GroupRepo{e.Store},
		Invitations: InvitationRepo{e.Store}, Access: e.Store, Usage: e.Store, Limits: lim, Accounts: e.Dir,
		Logos: e.Logos, Mailer: e.Mail, Events: e.Events, Audit: e.Audit, Tx: PassTx{},
		AppBaseURL: "https://app.vietgara.vn/", Now: func() time.Time { return e.Now },
	})

	return e
}
