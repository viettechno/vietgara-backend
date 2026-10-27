// Package platformstafftest has in-memory fakes of the platform staff ports,
// for the unit tests of the use case and the handler.
package platformstafftest

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
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
)

// Repo is an in-memory usecase.Repository.
type Repo struct {
	seq         atomic.Int64
	Roles       map[string]*domain.Role
	Groups      map[string]*domain.Group
	Members     map[string][]string // group -> users
	Admins      []string
	Invitations map[string]*domain.Invitation
	FailOn      string // method name that fails
}

// NewRepo returns an empty fake repository.
func NewRepo() *Repo {
	return &Repo{
		Roles: map[string]*domain.Role{}, Groups: map[string]*domain.Group{},
		Members: map[string][]string{}, Invitations: map[string]*domain.Invitation{},
	}
}

func (r *Repo) id(prefix string) string { return fmt.Sprintf("%s-%d", prefix, r.seq.Add(1)) }

// ErrFake is what a fake returns for the method named in FailOn.
var ErrFake = errors.New("fake failure")

func (r *Repo) fails(method string) error {
	if r.FailOn == method {
		return ErrFake
	}

	return nil
}

// ListRoles implements the Repo fake.
func (r *Repo) ListRoles(context.Context) ([]*domain.Role, error) {
	if err := r.fails("ListRoles"); err != nil {
		return nil, err
	}

	var out []*domain.Role
	for _, role := range r.Roles {
		out = append(out, r.withCount(role))
	}

	slices.SortFunc(out, func(a, b *domain.Role) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

func (r *Repo) withCount(role *domain.Role) *domain.Role {
	c := *role
	c.GroupCount = 0

	for _, g := range r.Groups {
		if g.RoleID == role.ID {
			c.GroupCount++
		}
	}

	return &c
}

// FindRole implements the Repo fake.
func (r *Repo) FindRole(_ context.Context, id string) (*domain.Role, error) {
	if err := r.fails("FindRole"); err != nil {
		return nil, err
	}

	role, ok := r.Roles[id]
	if !ok {
		return nil, domain.ErrRoleNotFound
	}

	return r.withCount(role), nil
}

// CreateRole implements the Repo fake.
func (r *Repo) CreateRole(_ context.Context, role *domain.Role) error {
	if err := r.fails("CreateRole"); err != nil {
		return err
	}

	for _, other := range r.Roles {
		if strings.EqualFold(other.Name, role.Name) {
			return domain.ErrRoleNameTaken
		}
	}

	role.ID = r.id("role")
	c := *role
	r.Roles[role.ID] = &c

	return nil
}

// UpdateRole implements the Repo fake.
func (r *Repo) UpdateRole(_ context.Context, role *domain.Role) error {
	if err := r.fails("UpdateRole"); err != nil {
		return err
	}

	c := *role
	r.Roles[role.ID] = &c

	return nil
}

// DeleteRole implements the Repo fake.
func (r *Repo) DeleteRole(_ context.Context, id string) error {
	if err := r.fails("DeleteRole"); err != nil {
		return err
	}

	for _, g := range r.Groups {
		if g.RoleID == id {
			return domain.ErrRoleInUse
		}
	}

	delete(r.Roles, id)

	return nil
}

// ListGroups implements the Repo fake.
func (r *Repo) ListGroups(context.Context) ([]*domain.Group, error) {
	if err := r.fails("ListGroups"); err != nil {
		return nil, err
	}

	var out []*domain.Group
	for _, g := range r.Groups {
		out = append(out, r.groupView(g))
	}

	return out, nil
}

func (r *Repo) groupView(g *domain.Group) *domain.Group {
	c := *g
	c.RoleName = r.Roles[g.RoleID].Name
	c.MemberCount = len(r.Members[g.ID])

	return &c
}

// FindGroup implements the Repo fake.
func (r *Repo) FindGroup(_ context.Context, id string) (*domain.Group, error) {
	if err := r.fails("FindGroup"); err != nil {
		return nil, err
	}

	g, ok := r.Groups[id]
	if !ok {
		return nil, domain.ErrGroupNotFound
	}

	return r.groupView(g), nil
}

// CreateGroup implements the Repo fake.
func (r *Repo) CreateGroup(_ context.Context, g *domain.Group) error {
	if err := r.fails("CreateGroup"); err != nil {
		return err
	}

	for _, other := range r.Groups {
		if strings.EqualFold(other.Name, g.Name) {
			return domain.ErrGroupNameTaken
		}
	}

	g.ID = r.id("group")
	c := *g
	r.Groups[g.ID] = &c

	return nil
}

// UpdateGroup implements the Repo fake.
func (r *Repo) UpdateGroup(_ context.Context, g *domain.Group) error {
	if err := r.fails("UpdateGroup"); err != nil {
		return err
	}

	c := *g
	r.Groups[g.ID] = &c

	return nil
}

// DeleteGroup implements the Repo fake.
func (r *Repo) DeleteGroup(_ context.Context, id string) error {
	if err := r.fails("DeleteGroup"); err != nil {
		return err
	}

	delete(r.Groups, id)
	delete(r.Members, id)

	for invID, inv := range r.Invitations {
		if inv.GroupID == id {
			delete(r.Invitations, invID)
		}
	}

	return nil
}

// GroupMembers implements the Repo fake.
func (r *Repo) GroupMembers(_ context.Context, groupID string) ([]string, error) {
	if err := r.fails("GroupMembers"); err != nil {
		return nil, err
	}

	return slices.Clone(r.Members[groupID]), nil
}

// Memberships implements the Repo fake.
func (r *Repo) Memberships(context.Context) (map[string][]domain.GroupRef, error) {
	if err := r.fails("Memberships"); err != nil {
		return nil, err
	}

	out := map[string][]domain.GroupRef{}

	for groupID, users := range r.Members {
		for _, u := range users {
			out[u] = append(out[u], domain.GroupRef{ID: groupID, Name: r.Groups[groupID].Name})
		}
	}

	return out, nil
}

// AdminIDs implements the Repo fake.
func (r *Repo) AdminIDs(context.Context) ([]string, error) {
	if err := r.fails("AdminIDs"); err != nil {
		return nil, err
	}

	return slices.Clone(r.Admins), nil
}

// IsAdmin implements the Repo fake.
func (r *Repo) IsAdmin(_ context.Context, userID string) (bool, error) {
	if err := r.fails("IsAdmin"); err != nil {
		return false, err
	}

	return slices.Contains(r.Admins, userID), nil
}

// AddMember implements the Repo fake.
func (r *Repo) AddMember(_ context.Context, groupID, userID string) error {
	if err := r.fails("AddMember"); err != nil {
		return err
	}

	r.Members[groupID] = append(r.Members[groupID], userID)

	return nil
}

// RemoveMember implements the Repo fake.
func (r *Repo) RemoveMember(_ context.Context, groupID, userID string) error {
	if err := r.fails("RemoveMember"); err != nil {
		return err
	}

	i := slices.Index(r.Members[groupID], userID)
	if i < 0 {
		return domain.ErrMemberNotFound
	}

	r.Members[groupID] = slices.Delete(r.Members[groupID], i, i+1)

	return nil
}

// IsMember implements the Repo fake.
func (r *Repo) IsMember(_ context.Context, groupID, userID string) (bool, error) {
	if err := r.fails("IsMember"); err != nil {
		return false, err
	}

	return slices.Contains(r.Members[groupID], userID), nil
}

// CreateInvitation implements the Repo fake.
func (r *Repo) CreateInvitation(_ context.Context, inv *domain.Invitation) error {
	if err := r.fails("CreateInvitation"); err != nil {
		return err
	}

	for _, other := range r.Invitations {
		if other.GroupID == inv.GroupID && other.UserID == inv.UserID && other.Status == domain.InvitationPending {
			return domain.ErrAlreadyInvited
		}
	}

	inv.ID = r.id("inv")
	c := *inv
	r.Invitations[inv.ID] = &c

	return nil
}

func (r *Repo) invitationView(inv *domain.Invitation) *domain.Invitation {
	c := *inv
	if g, ok := r.Groups[inv.GroupID]; ok {
		c.GroupName, c.RoleName = g.Name, r.Roles[g.RoleID].Name
	}

	return &c
}

// FindInvitation implements the Repo fake.
func (r *Repo) FindInvitation(_ context.Context, id string) (*domain.Invitation, error) {
	if err := r.fails("FindInvitation"); err != nil {
		return nil, err
	}

	inv, ok := r.Invitations[id]
	if !ok {
		return nil, domain.ErrInvitationNotFound
	}

	return r.invitationView(inv), nil
}

// UpdateInvitation implements the Repo fake.
func (r *Repo) UpdateInvitation(_ context.Context, inv *domain.Invitation) error {
	if err := r.fails("UpdateInvitation"); err != nil {
		return err
	}

	c := *inv
	r.Invitations[inv.ID] = &c

	return nil
}

// ListInvitations implements the Repo fake.
func (r *Repo) ListInvitations(context.Context) ([]*domain.Invitation, error) {
	if err := r.fails("ListInvitations"); err != nil {
		return nil, err
	}

	var out []*domain.Invitation
	for _, inv := range r.Invitations {
		out = append(out, r.invitationView(inv))
	}

	slices.SortFunc(out, func(a, b *domain.Invitation) int { return strings.Compare(a.ID, b.ID) })

	return out, nil
}

// ListInvitationsByUser implements the Repo fake.
func (r *Repo) ListInvitationsByUser(_ context.Context, userID string) ([]*domain.Invitation, error) {
	if err := r.fails("ListInvitationsByUser"); err != nil {
		return nil, err
	}

	var out []*domain.Invitation

	for _, inv := range r.Invitations {
		if inv.UserID == userID {
			out = append(out, r.invitationView(inv))
		}
	}

	return out, nil
}

// ExpireStale implements the Repo fake.
func (r *Repo) ExpireStale(_ context.Context, groupID, userID string, now time.Time) error {
	if err := r.fails("ExpireStale"); err != nil {
		return err
	}

	for _, inv := range r.Invitations {
		if inv.GroupID == groupID && inv.UserID == userID && inv.Status == domain.InvitationPending && !now.Before(inv.ExpiresAt) {
			inv.Status = domain.InvitationExpired
		}
	}

	return nil
}

// PlatformAccess implements the Repo fake.
func (r *Repo) PlatformAccess(_ context.Context, userID string) (*access.PlatformAccess, error) {
	if err := r.fails("PlatformAccess"); err != nil {
		return nil, err
	}

	acc := &access.PlatformAccess{UserID: userID, Admin: slices.Contains(r.Admins, userID)}

	for groupID, users := range r.Members {
		if slices.Contains(users, userID) {
			acc.Permissions = append(acc.Permissions, r.Roles[r.Groups[groupID].RoleID].Permissions...)
		}
	}

	acc.Permissions = access.NormalizePlatform(acc.Permissions)

	return acc, nil
}

// Users is an in-memory usecase.UserDirectory.
type Users struct {
	ByID  map[string]*usecase.UserSummary
	Fails bool
}

// FindByEmail implements the Users fake.
func (u *Users) FindByEmail(_ context.Context, email string) (*usecase.UserSummary, error) {
	for _, user := range u.ByID {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

// ListByIDs implements the Users fake.
func (u *Users) ListByIDs(_ context.Context, ids []string) (map[string]*usecase.UserSummary, error) {
	if u.Fails {
		return nil, ErrFake
	}

	out := map[string]*usecase.UserSummary{}

	for _, id := range ids {
		if user, ok := u.ByID[id]; ok {
			out[id] = user
		}
	}

	return out, nil
}

// Recorder collects audit entries and sent messages.
type Recorder struct {
	Entries   []audit.Entry
	Sent      []mail.Message
	FailAudit bool
}

// Record implements the Recorder fake.
func (r *Recorder) Record(_ context.Context, e audit.Entry) error {
	if r.FailAudit {
		return ErrFake
	}

	r.Entries = append(r.Entries, e)

	return nil
}

// Send implements the Recorder fake.
func (r *Recorder) Send(_ context.Context, m mail.Message) error {
	r.Sent = append(r.Sent, m)

	return nil
}

// Actions implements the Recorder fake.
func (r *Recorder) Actions() []string {
	out := make([]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e.Action)
	}

	return out
}
