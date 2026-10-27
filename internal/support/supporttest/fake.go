// Package supporttest has in-memory fakes of the support ports, for the unit
// tests of the use case and the handler.
package supporttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
)

// ErrFake is what a fake returns for the method named in Repo.FailOn.
var ErrFake = errors.New("fake failure")

// Repo is an in-memory usecase.Repository.
type Repo struct {
	seq      atomic.Int64
	Grants   map[string]*domain.Grant
	Garages  map[string]*domain.Garage
	Events   []*domain.AccessEvent
	Outbox   []*domain.OutboxEvent
	Notified []*domain.Notification
	ReadOnly bool
	FailOn   string
}

// NewRepo returns an empty fake repository.
func NewRepo() *Repo {
	return &Repo{Grants: map[string]*domain.Grant{}, Garages: map[string]*domain.Garage{}}
}

func (r *Repo) fails(method string) error {
	if r.FailOn == method {
		return ErrFake
	}

	return nil
}

// Create implements the repository.
func (r *Repo) Create(_ context.Context, g *domain.Grant) error {
	if err := r.fails("Create"); err != nil {
		return err
	}

	g.ID = fmt.Sprintf("grant-%d", r.seq.Add(1))
	g.StaffName, g.StaffEmail, g.StaffLocale = "Staff "+g.StaffUserID, g.StaffUserID+"@x.test", "en"

	if garage, ok := r.Garages[g.GarageID]; ok {
		g.GarageName = garage.Name
	}

	clone := *g
	r.Grants[g.ID] = &clone

	return nil
}

// Update implements the repository.
func (r *Repo) Update(_ context.Context, g *domain.Grant) error {
	if err := r.fails("Update"); err != nil {
		return err
	}

	if _, ok := r.Grants[g.ID]; !ok {
		return domain.ErrGrantNotFound
	}

	clone := *g
	r.Grants[g.ID] = &clone

	return nil
}

// Find implements the repository.
func (r *Repo) Find(_ context.Context, id string) (*domain.Grant, error) {
	if err := r.fails("Find"); err != nil {
		return nil, err
	}

	g, ok := r.Grants[id]
	if !ok {
		return nil, domain.ErrGrantNotFound
	}

	clone := *g

	return &clone, nil
}

func (r *Repo) list(keep func(*domain.Grant) bool) []*domain.Grant {
	var out []*domain.Grant

	for _, g := range r.Grants {
		if keep(g) {
			clone := *g
			out = append(out, &clone)
		}
	}

	slices.SortFunc(out, func(a, b *domain.Grant) int { return strings.Compare(a.ID, b.ID) })

	return out
}

// ListByStaff implements the repository.
func (r *Repo) ListByStaff(_ context.Context, staffUserID string) ([]*domain.Grant, error) {
	if err := r.fails("ListByStaff"); err != nil {
		return nil, err
	}

	return r.list(func(g *domain.Grant) bool { return g.StaffUserID == staffUserID }), nil
}

// ListByGarage implements the repository.
func (r *Repo) ListByGarage(_ context.Context, garageID string) ([]*domain.Grant, error) {
	if err := r.fails("ListByGarage"); err != nil {
		return nil, err
	}

	return r.list(func(g *domain.Grant) bool { return g.GarageID == garageID }), nil
}

// HasOpen implements the repository.
func (r *Repo) HasOpen(_ context.Context, garageID, staffUserID string, now time.Time) (bool, error) {
	if err := r.fails("HasOpen"); err != nil {
		return false, err
	}

	return len(r.list(func(g *domain.Grant) bool {
		status := g.Status(now)

		return g.GarageID == garageID && g.StaffUserID == staffUserID && (status == domain.StatusPending || status == domain.StatusActive)
	})) > 0, nil
}

// ActiveGrant implements the repository.
func (r *Repo) ActiveGrant(_ context.Context, garageID, staffUserID string, now time.Time) (grantID, ownerUserID string, err error) {
	if err := r.fails("ActiveGrant"); err != nil {
		return "", "", err
	}

	for _, g := range r.list(func(g *domain.Grant) bool {
		return g.GarageID == garageID && g.StaffUserID == staffUserID && g.Status(now) == domain.StatusActive
	}) {
		return g.ID, r.Garages[garageID].OwnerUserID, nil
	}

	return "", "", domain.ErrGrantNotFound
}

// Garage implements the repository.
func (r *Repo) Garage(_ context.Context, garageID string) (*domain.Garage, error) {
	if err := r.fails("Garage"); err != nil {
		return nil, err
	}

	garage, ok := r.Garages[garageID]
	if !ok {
		return nil, domain.ErrGarageNotFound
	}

	return garage, nil
}

// Directory implements the repository.
func (r *Repo) Directory(_ context.Context, query string, _ domain.Page) ([]*domain.DirectoryEntry, int, error) {
	if err := r.fails("Directory"); err != nil {
		return nil, 0, err
	}

	var out []*domain.DirectoryEntry

	for _, g := range r.Garages {
		if strings.Contains(strings.ToLower(g.Name), strings.ToLower(query)) {
			out = append(out, &domain.DirectoryEntry{GarageID: g.ID, Name: g.Name, OwnerName: g.OwnerName, OwnerEmail: g.OwnerEmail, Active: true})
		}
	}

	return out, len(out), nil
}

// InsertEvent implements the repository.
func (r *Repo) InsertEvent(_ context.Context, e *domain.AccessEvent) error {
	if err := r.fails("InsertEvent"); err != nil {
		return err
	}

	e.ID = fmt.Sprintf("event-%d", r.seq.Add(1))
	clone := *e
	r.Events = append(r.Events, &clone)

	return nil
}

// ListEvents implements the repository.
func (r *Repo) ListEvents(_ context.Context, garageID, grantID string, _ domain.Page) ([]*domain.AccessEvent, int, error) {
	if err := r.fails("ListEvents"); err != nil {
		return nil, 0, err
	}

	var out []*domain.AccessEvent

	for _, e := range r.Events {
		if e.GarageID == garageID && (grantID == "" || e.GrantID == grantID) {
			out = append(out, e)
		}
	}

	return out, len(out), nil
}

// ListOutbox implements the repository.
func (r *Repo) ListOutbox(_ context.Context, _, _ string, _ domain.Page) ([]*domain.OutboxEvent, int, error) {
	if err := r.fails("ListOutbox"); err != nil {
		return nil, 0, err
	}

	return r.Outbox, len(r.Outbox), nil
}

// ListNotifications implements the repository.
func (r *Repo) ListNotifications(_ context.Context, _ string, _ domain.Page) ([]*domain.Notification, int, error) {
	if err := r.fails("ListNotifications"); err != nil {
		return nil, 0, err
	}

	return r.Notified, len(r.Notified), nil
}

// SetReadOnly implements the repository.
func (r *Repo) SetReadOnly(context.Context) error {
	if err := r.fails("SetReadOnly"); err != nil {
		return err
	}

	r.ReadOnly = true

	return nil
}

// Audit is an in-memory audit logger.
type Audit struct {
	Entries []audit.Entry
	Err     error
}

// Record implements the logger.
func (a *Audit) Record(_ context.Context, e audit.Entry) error {
	if a.Err != nil {
		return a.Err
	}

	a.Entries = append(a.Entries, e)

	return nil
}

// Actions returns the audited actions in order.
func (a *Audit) Actions() []string {
	out := make([]string, 0, len(a.Entries))
	for _, e := range a.Entries {
		out = append(out, e.Action)
	}

	return out
}

// Mailer collects sent messages.
type Mailer struct {
	Sent []mail.Message
	Err  error
}

// Send implements mail.Sender.
func (m *Mailer) Send(_ context.Context, msg mail.Message) error {
	if m.Err != nil {
		return m.Err
	}

	m.Sent = append(m.Sent, msg)

	return nil
}

// Platform is a fixed access.PlatformReader.
type Platform struct {
	Access map[string]*access.PlatformAccess
	Err    error
}

// PlatformAccess implements access.PlatformReader.
func (p *Platform) PlatformAccess(_ context.Context, userID string) (*access.PlatformAccess, error) {
	if p.Err != nil {
		return nil, p.Err
	}

	if a, ok := p.Access[userID]; ok {
		return a, nil
	}

	return &access.PlatformAccess{UserID: userID}, nil
}

// Tx runs fn directly and records the scopes it was asked for.
type Tx struct {
	Scopes []platformdb.Scope
	Err    error
}

// WithTransaction implements middleware.Transactor.
func (t *Tx) WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error {
	t.Scopes = append(t.Scopes, scope)

	if t.Err != nil {
		return t.Err
	}

	return fn(ctx)
}
