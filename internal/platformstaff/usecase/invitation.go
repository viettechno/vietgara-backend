package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
)

// CreateInvitation invites an existing, verified user into a group and
// e-mails them once the invitation is stored. The group's role may not hold
// a permission the caller lacks, and a user cannot invite themselves.
func (uc *UseCase) CreateInvitation(ctx context.Context, rawEmail, groupID string) (*domain.Invitation, error) {
	group, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return nil, toAppError("get platform group", err)
	}

	role, err := uc.Repo.FindRole(ctx, group.RoleID)
	if err != nil {
		return nil, toAppError("get platform role", err)
	}

	if err := requireHeld(ctx, role.Permissions); err != nil {
		return nil, err
	}

	invitee, err := uc.checkInvitee(ctx, rawEmail, groupID)
	if err != nil {
		return nil, err
	}

	if err := uc.Repo.ExpireStale(ctx, groupID, invitee.ID, uc.now()); err != nil {
		return nil, toAppError("expire invitations", err)
	}

	inv := &domain.Invitation{
		GroupID: groupID, UserID: invitee.ID, Status: domain.InvitationPending,
		InvitedBy: middleware.UserID(ctx), ExpiresAt: uc.now().Add(domain.InvitationTTL),
	}

	if err := uc.Repo.CreateInvitation(ctx, inv); err != nil {
		return nil, toAppError("create invitation", err)
	}

	created, err := uc.loadInvitation(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, "platform_staff.invited", "platform_invitation", created.ID, nil, created); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.sendInvitation(context.WithoutCancel(ctx), created, invitee) })

	return created, nil
}

// checkInvitee returns the invited user: it must exist, be active and
// verified, not be the caller, not be a Platform Admin and not already be in
// the group.
func (uc *UseCase) checkInvitee(ctx context.Context, rawEmail, groupID string) (*UserSummary, error) {
	invitee, err := uc.Users.FindByEmail(ctx, rawEmail)
	if err != nil {
		return nil, toAppError("find invitee", err)
	}

	if !invitee.Active || !invitee.EmailVerified {
		return nil, toAppError("invite", domain.ErrUserInactive)
	}

	if invitee.ID == middleware.UserID(ctx) {
		return nil, toAppError("invite", domain.ErrCannotInviteSelf)
	}

	admin, err := uc.Repo.IsAdmin(ctx, invitee.ID)
	if err != nil {
		return nil, toAppError("check platform admin", err)
	}

	if admin {
		return nil, toAppError("invite", domain.ErrAlreadyAdmin)
	}

	member, err := uc.Repo.IsMember(ctx, groupID, invitee.ID)
	if err != nil {
		return nil, toAppError("check membership", err)
	}

	if member {
		return nil, toAppError("invite", domain.ErrAlreadyMember)
	}

	return invitee, nil
}

func (uc *UseCase) sendInvitation(ctx context.Context, inv *domain.Invitation, invitee *UserSummary) {
	layout := "02/01/2006"
	if invitee.Locale == mail.LocaleEN {
		layout = "Jan 2, 2006"
	}

	msg, err := mail.Render(mail.TemplatePlatformStaffInvitation, invitee.Locale, invitee.Email, invitee.FullName, map[string]any{
		"Name":        invitee.FullName,
		"GroupName":   inv.GroupName,
		"InviterName": inv.InvitedByName,
		"AcceptURL":   strings.TrimSuffix(uc.AdminBaseURL, "/") + "/invitations/" + inv.ID,
		"ExpiresAt":   inv.ExpiresAt.Format(layout),
	})
	if err == nil {
		err = uc.Mailer.Send(ctx, msg)
	}

	if err != nil {
		slog.Warn("platformstaff: sending the invitation e-mail failed", "invitation_id", inv.ID, "error", err)
	}
}

// ListInvitations returns the invitations to platform groups, optionally
// filtered by (effective) status.
func (uc *UseCase) ListInvitations(ctx context.Context, status domain.InvitationStatus) ([]*domain.Invitation, error) {
	invitations, err := uc.Repo.ListInvitations(ctx)
	if err != nil {
		return nil, toAppError("list invitations", err)
	}

	if err := uc.withUsers(ctx, invitations...); err != nil {
		return nil, err
	}

	return uc.filterInvitations(invitations, status), nil
}

// RevokeInvitation cancels a pending invitation.
func (uc *UseCase) RevokeInvitation(ctx context.Context, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.loadInvitation(ctx, invitationID)
	if err != nil {
		return nil, err
	}

	group, err := uc.Repo.FindGroup(ctx, inv.GroupID)
	if err != nil {
		return nil, toAppError("get platform group", err)
	}

	role, err := uc.Repo.FindRole(ctx, group.RoleID)
	if err != nil {
		return nil, toAppError("get platform role", err)
	}

	if err := requireHeld(ctx, role.Permissions); err != nil {
		return nil, err
	}

	if err := inv.Revoke(uc.now()); err != nil {
		return nil, toAppError("revoke invitation", err)
	}

	if err := uc.Repo.UpdateInvitation(ctx, inv); err != nil {
		return nil, toAppError("revoke invitation", err)
	}

	if err := uc.audit(ctx, "platform_staff.invitation_revoked", "platform_invitation", inv.ID, nil, inv); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	return inv, nil
}

// ListMyInvitations returns the invitations sent to the caller.
func (uc *UseCase) ListMyInvitations(ctx context.Context, status domain.InvitationStatus) ([]*domain.Invitation, error) {
	invitations, err := uc.Repo.ListInvitationsByUser(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, toAppError("list invitations", err)
	}

	if err := uc.withUsers(ctx, invitations...); err != nil {
		return nil, err
	}

	return uc.filterInvitations(invitations, status), nil
}

// AcceptInvitation joins the group of the caller's pending invitation. The
// new permissions apply from the caller's next request.
func (uc *UseCase) AcceptInvitation(ctx context.Context, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.myInvitation(ctx, invitationID)
	if err != nil {
		return nil, err
	}

	if err := inv.Accept(uc.now()); err != nil {
		return nil, toAppError("accept invitation", err)
	}

	if _, err := uc.Repo.FindGroup(ctx, inv.GroupID); err != nil {
		return nil, toAppError("get platform group", err)
	}

	member, err := uc.Repo.IsMember(ctx, inv.GroupID, inv.UserID)
	if err != nil {
		return nil, toAppError("check membership", err)
	}

	if !member {
		if err := uc.Repo.AddMember(ctx, inv.GroupID, inv.UserID); err != nil {
			return nil, toAppError("join group", err)
		}
	}

	if err := uc.Repo.UpdateInvitation(ctx, inv); err != nil {
		return nil, toAppError("accept invitation", err)
	}

	if err := uc.audit(ctx, "platform_staff.joined", "platform_invitation", inv.ID, nil, inv); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	return inv, nil
}

// DeclineInvitation declines the caller's pending invitation.
func (uc *UseCase) DeclineInvitation(ctx context.Context, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.myInvitation(ctx, invitationID)
	if err != nil {
		return nil, err
	}

	if err := inv.Decline(uc.now()); err != nil {
		return nil, toAppError("decline invitation", err)
	}

	if err := uc.Repo.UpdateInvitation(ctx, inv); err != nil {
		return nil, toAppError("decline invitation", err)
	}

	if err := uc.audit(ctx, "platform_staff.invitation_declined", "platform_invitation", inv.ID, nil, inv); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	return inv, nil
}

// myInvitation loads an invitation addressed to the caller; anyone else's is
// reported as not found.
func (uc *UseCase) myInvitation(ctx context.Context, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.loadInvitation(ctx, invitationID)
	if err != nil {
		return nil, err
	}

	if inv.UserID != middleware.UserID(ctx) {
		return nil, toAppError("get invitation", domain.ErrInvitationNotFound)
	}

	return inv, nil
}

func (uc *UseCase) loadInvitation(ctx context.Context, id string) (*domain.Invitation, error) {
	inv, err := uc.Repo.FindInvitation(ctx, id)
	if err != nil {
		return nil, toAppError("get invitation", err)
	}

	if err := uc.withUsers(ctx, inv); err != nil {
		return nil, err
	}

	return inv, nil
}

// withUsers fills the display names of the invitee and the inviter.
func (uc *UseCase) withUsers(ctx context.Context, invitations ...*domain.Invitation) error {
	if len(invitations) == 0 {
		return nil
	}

	ids := make([]string, 0, 2*len(invitations))
	for _, inv := range invitations {
		ids = append(ids, inv.UserID, inv.InvitedBy)
	}

	users, err := uc.Users.ListByIDs(ctx, ids)
	if err != nil {
		return toAppError("load invitation users", err)
	}

	for _, inv := range invitations {
		if user, ok := users[inv.UserID]; ok {
			inv.FullName, inv.Email = user.FullName, user.Email
		}

		if user, ok := users[inv.InvitedBy]; ok {
			inv.InvitedByName = user.FullName
		}
	}

	return nil
}

func (uc *UseCase) filterInvitations(invitations []*domain.Invitation, status domain.InvitationStatus) []*domain.Invitation {
	now := uc.now()
	out := make([]*domain.Invitation, 0, len(invitations))

	for _, inv := range invitations {
		effective := inv.EffectiveStatus(now)
		if status != "" && effective != status {
			continue
		}

		inv.Status = effective
		out = append(out, inv)
	}

	return out
}
