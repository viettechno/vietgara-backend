// Package usecase implements platform roles, staff groups, staff and
// invitations (Release 8.1, FR-IAM-10). Every platform RPC runs in the
// request's transaction after the interceptor has checked the platform
// permission, so these methods only add the rules the permission cannot
// express: names are unique, what is in use stays, and a user who is not a
// Platform Admin can only hand out permissions they hold themselves.
package usecase

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// UserSummary is the part of an Identity user this module needs.
type UserSummary struct {
	ID            string
	FullName      string
	Email         string
	Locale        string
	Active        bool
	EmailVerified bool
}

// UserDirectory is a read-only view of Identity users, implemented by an
// adapter in cmd/server so the modules stay decoupled. FindByEmail returns
// domain.ErrUserNotFound.
type UserDirectory interface {
	FindByEmail(ctx context.Context, email string) (*UserSummary, error)
	ListByIDs(ctx context.Context, ids []string) (map[string]*UserSummary, error)
}

// Repository stores the platform staff model.
type Repository interface {
	ListRoles(ctx context.Context) ([]*domain.Role, error)
	FindRole(ctx context.Context, id string) (*domain.Role, error)
	CreateRole(ctx context.Context, role *domain.Role) error
	UpdateRole(ctx context.Context, role *domain.Role) error
	// DeleteRole returns domain.ErrRoleInUse while a group uses the role.
	DeleteRole(ctx context.Context, id string) error

	ListGroups(ctx context.Context) ([]*domain.Group, error)
	FindGroup(ctx context.Context, id string) (*domain.Group, error)
	CreateGroup(ctx context.Context, group *domain.Group) error
	UpdateGroup(ctx context.Context, group *domain.Group) error
	// DeleteGroup removes the group, its memberships and its invitations.
	DeleteGroup(ctx context.Context, id string) error

	// GroupMembers returns the user ids of a group, oldest first.
	GroupMembers(ctx context.Context, groupID string) ([]string, error)
	// Memberships returns, for every user in a group, the groups they are in.
	Memberships(ctx context.Context) (map[string][]domain.GroupRef, error)
	// AdminIDs returns the Platform Admins' user ids.
	AdminIDs(ctx context.Context) ([]string, error)
	IsAdmin(ctx context.Context, userID string) (bool, error)
	AddMember(ctx context.Context, groupID, userID string) error
	// RemoveMember returns domain.ErrMemberNotFound.
	RemoveMember(ctx context.Context, groupID, userID string) error
	IsMember(ctx context.Context, groupID, userID string) (bool, error)

	CreateInvitation(ctx context.Context, inv *domain.Invitation) error
	FindInvitation(ctx context.Context, id string) (*domain.Invitation, error)
	UpdateInvitation(ctx context.Context, inv *domain.Invitation) error
	ListInvitations(ctx context.Context) ([]*domain.Invitation, error)
	ListInvitationsByUser(ctx context.Context, userID string) ([]*domain.Invitation, error)
	// ExpireStale marks the user's pending invitations to the group that are
	// past their expiry as expired, so a new one can be made.
	ExpireStale(ctx context.Context, groupID, userID string, now time.Time) error

	// PlatformAccess implements access.PlatformReader.
	PlatformAccess(ctx context.Context, userID string) (*access.PlatformAccess, error)
}

// AuditLogger records changes in the audit log, in the request's
// transaction.
type AuditLogger interface {
	Record(ctx context.Context, entry audit.Entry) error
}

// Deps are the dependencies of UseCase.
type Deps struct {
	Repo   Repository
	Users  UserDirectory
	Audit  AuditLogger
	Mailer mail.Sender
	// AdminBaseURL is the platform admin web, used in e-mailed links.
	AdminBaseURL string
	// Now defaults to time.Now.
	Now func() time.Time
}

// UseCase implements every action of the module.
type UseCase struct {
	Deps
}

// New wires the use case.
func New(deps Deps) *UseCase {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &UseCase{Deps: deps}
}

func (uc *UseCase) now() time.Time { return uc.Now().UTC() }

// audit records a change made by the caller; platform entries have no garage.
func (uc *UseCase) audit(ctx context.Context, action, entityType, entityID string, before, after any) error {
	return uc.Audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
}

// PermissionCatalogue returns the catalogue in display order.
func (uc *UseCase) PermissionCatalogue() []access.PlatformCatalogEntry {
	return access.PlatformCatalog
}

// MyAccess returns what the caller may do on the platform and how many
// invitations wait for an answer. It needs no platform permission.
func (uc *UseCase) MyAccess(ctx context.Context) (*access.PlatformAccess, int, error) {
	userID := middleware.UserID(ctx)

	acc, err := uc.Repo.PlatformAccess(ctx, userID)
	if err != nil {
		return nil, 0, toAppError("load platform access", err)
	}

	invitations, err := uc.Repo.ListInvitationsByUser(ctx, userID)
	if err != nil {
		return nil, 0, toAppError("list invitations", err)
	}

	pending := 0

	now := uc.now()
	for _, inv := range invitations {
		if inv.EffectiveStatus(now) == domain.InvitationPending {
			pending++
		}
	}

	return acc, pending, nil
}

// requireHeld refuses permissions the caller does not hold: only a Platform
// Admin may give a permission they do not have themselves, so a holder of
// platform_staff.write cannot raise their own (or a friend's) access.
func requireHeld(ctx context.Context, permissions []access.PlatformPermission) error {
	acc, ok := access.PlatformFromContext(ctx)
	if !ok {
		return apperror.New(apperror.CodePermissionDenied, access.ErrPlatformDenied.Error())
	}

	for _, p := range permissions {
		if !acc.Has(p) {
			return apperror.Wrap(apperror.CodePermissionDenied, domain.ErrPermissionNotHeld.Error(), domain.ErrPermissionNotHeld).
				WithReason(domain.ReasonPermissionNotHeld)
		}
	}

	return nil
}

// toAppError classifies err into an *apperror.AppError; op names the failed
// operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	type mapping struct {
		target error
		code   apperror.Code
		reason string
	}

	for _, m := range []mapping{
		{domain.ErrRoleNotFound, apperror.CodeNotFound, ""},
		{domain.ErrGroupNotFound, apperror.CodeNotFound, ""},
		{domain.ErrInvitationNotFound, apperror.CodeNotFound, ""},
		{domain.ErrMemberNotFound, apperror.CodeNotFound, ""},
		{domain.ErrUserNotFound, apperror.CodeNotFound, domain.ReasonUserNotFound},
		{domain.ErrUserInactive, apperror.CodeFailedPrecondition, domain.ReasonUserInactive},
		{domain.ErrRoleNameTaken, apperror.CodeAlreadyExists, domain.ReasonRoleNameTaken},
		{domain.ErrRoleInUse, apperror.CodeFailedPrecondition, domain.ReasonRoleInUse},
		{domain.ErrGroupNameTaken, apperror.CodeAlreadyExists, domain.ReasonGroupNameTaken},
		{domain.ErrAlreadyMember, apperror.CodeAlreadyExists, domain.ReasonAlreadyMember},
		{domain.ErrAlreadyInvited, apperror.CodeAlreadyExists, domain.ReasonAlreadyInvited},
		{domain.ErrAlreadyAdmin, apperror.CodeFailedPrecondition, domain.ReasonAlreadyAdmin},
		{domain.ErrCannotInviteSelf, apperror.CodeFailedPrecondition, domain.ReasonCannotInviteSelf},
		{domain.ErrInvitationNotPending, apperror.CodeFailedPrecondition, domain.ReasonInvitationNotPending},
		{domain.ErrInvitationExpired, apperror.CodeFailedPrecondition, domain.ReasonInvitationExpired},
	} {
		if errors.Is(err, m.target) {
			appErr := apperror.Wrap(m.code, m.target.Error(), err)
			if m.reason != "" {
				appErr = appErr.WithReason(m.reason)
			}

			return appErr
		}
	}

	return apperror.Internal(op, err)
}

func containsID(ids []string, id string) bool { return slices.Contains(ids, id) }
