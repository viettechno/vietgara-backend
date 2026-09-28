package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// CreateInvitation invites an existing account to the garage with the
// given groups (FR-TEN-04), within the plan's staff limit, and e-mails the
// invitee once the invitation is stored.
func (uc *TenantUseCase) CreateInvitation(ctx context.Context, garageID, accountID string, groupIDs []string) (*domain.Invitation, error) {
	invitee, err := uc.checkInvitee(ctx, garageID, accountID)
	if err != nil {
		return nil, err
	}

	groupIDs, err = uc.validGroupIDs(ctx, garageID, groupIDs)
	if err != nil {
		return nil, err
	}

	if err := uc.Invitations.ExpireStale(ctx, garageID, accountID); err != nil {
		return nil, toAppError("expire invitations", err)
	}

	if err := uc.checkGarageOwnerUsage(ctx, garageID, LimitStaff); err != nil {
		return nil, err
	}

	now := uc.now()
	inv := &domain.Invitation{
		GarageID: garageID, AccountID: accountID, GroupIDs: groupIDs, Status: domain.InvitationPending,
		InvitedBy: middleware.AccountID(ctx), ExpiresAt: now.Add(domain.InvitationTTL),
	}

	if err := uc.Invitations.Create(ctx, inv); err != nil {
		return nil, toAppError("create invitation", err)
	}

	created, err := uc.Invitations.FindByID(ctx, inv.ID)
	if err != nil {
		return nil, toAppError("reload invitation", err)
	}

	if err := uc.withInvitationAccounts(ctx, created); err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, garageID, "staff.invited", "invitation", inv.ID, nil, created); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	if err := uc.Events.Publish(ctx, EventStaffInvited, garageID, accountID, map[string]any{
		"invitation_id": inv.ID, keyGarageID: garageID, "account_id": accountID,
	}); err != nil {
		return nil, toAppError("publish invitation", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.sendInvitation(context.WithoutCancel(ctx), created, invitee) })

	return created, nil
}

// checkInvitee returns the invited account: it must be active and not
// already an active member of the garage.
func (uc *TenantUseCase) checkInvitee(ctx context.Context, garageID, accountID string) (*AccountSummary, error) {
	invitee, err := uc.Accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, toAppError("find invitee", err)
	}

	if !invitee.Active {
		return nil, toAppError("invite", domain.ErrAccountInactive)
	}

	member, err := uc.Staff.FindByAccount(ctx, garageID, accountID)
	if err == nil && member.Status == domain.StaffStatusActive {
		return nil, toAppError("invite", domain.ErrAlreadyMember)
	}

	if err != nil && !errors.Is(err, domain.ErrStaffNotFound) {
		return nil, toAppError("find membership", err)
	}

	return invitee, nil
}

func (uc *TenantUseCase) sendInvitation(ctx context.Context, inv *domain.Invitation, invitee *AccountSummary) {
	locale := invitee.Locale
	layout := "02/01/2006"

	if locale == mail.LocaleEN {
		layout = "Jan 2, 2006"
	}

	msg, err := mail.Render(mail.TemplateStaffInvitation, locale, invitee.Email, invitee.FullName, map[string]any{
		"Name":        invitee.FullName,
		"GarageName":  inv.GarageName,
		"InviterName": inv.InvitedByName,
		"AcceptURL":   strings.TrimSuffix(uc.AppBaseURL, "/") + "/invitations/" + inv.ID,
		"ExpiresAt":   inv.ExpiresAt.Format(layout),
	})
	if err == nil {
		err = uc.Mailer.Send(ctx, msg)
	}

	if err != nil {
		slog.Warn("tenant: sending the invitation e-mail failed", "invitation_id", inv.ID, "error", err)
	}
}

// ListInvitations returns the garage's invitations, optionally filtered by
// (effective) status.
func (uc *TenantUseCase) ListInvitations(ctx context.Context, garageID string, status domain.InvitationStatus) ([]*domain.Invitation, error) {
	invitations, err := uc.Invitations.ListByGarage(ctx, garageID)
	if err != nil {
		return nil, toAppError("list invitations", err)
	}

	if err := uc.withInvitationAccounts(ctx, invitations...); err != nil {
		return nil, err
	}

	return uc.filterInvitations(invitations, status), nil
}

// RevokeInvitation cancels a pending invitation of the garage.
func (uc *TenantUseCase) RevokeInvitation(ctx context.Context, garageID, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.Invitations.FindByID(ctx, invitationID)
	if err != nil || inv.GarageID != garageID {
		return nil, toAppError("revoke invitation", domain.ErrInvitationNotFound)
	}

	if err := inv.Revoke(uc.now()); err != nil {
		return nil, toAppError("revoke invitation", err)
	}

	if err := uc.Invitations.Update(ctx, inv); err != nil {
		return nil, toAppError("revoke invitation", err)
	}

	if err := uc.audit(ctx, garageID, "staff.invitation_revoked", "invitation", inv.ID, nil, inv); err != nil {
		return nil, toAppError("audit invitation", err)
	}

	return inv, nil
}

// ListMyInvitations returns the invitations sent to accountID.
func (uc *TenantUseCase) ListMyInvitations(ctx context.Context, accountID string, status domain.InvitationStatus) ([]*domain.Invitation, error) {
	invitations, err := uc.Invitations.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, toAppError("list invitations", err)
	}

	if err := uc.withInvitationAccounts(ctx, invitations...); err != nil {
		return nil, err
	}

	return uc.filterInvitations(invitations, status), nil
}

// GetMyInvitation returns one invitation sent to accountID.
func (uc *TenantUseCase) GetMyInvitation(ctx context.Context, accountID, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.Invitations.FindByID(ctx, invitationID)
	if err != nil || inv.AccountID != accountID {
		return nil, toAppError("get invitation", domain.ErrInvitationNotFound)
	}

	inv.Status = inv.EffectiveStatus(uc.now())

	if err := uc.withInvitationAccounts(ctx, inv); err != nil {
		return nil, err
	}

	return inv, nil
}

// AcceptInvitation joins the garage with the invitation's groups (those
// that still exist), reactivating a previous membership if there is one.
func (uc *TenantUseCase) AcceptInvitation(ctx context.Context, accountID, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.GetMyInvitation(ctx, accountID, invitationID)
	if err != nil {
		return nil, err
	}

	if err := inv.Accept(uc.now()); err != nil {
		return nil, toAppError("accept invitation", err)
	}

	err = uc.Tx.WithTransaction(ctx, platformdb.AccountScope(accountID).Garage(inv.GarageID), func(ctx context.Context) error {
		if err := uc.Invitations.Update(ctx, inv); err != nil {
			return err
		}

		member, err := uc.joinGarage(ctx, inv)
		if err != nil {
			return err
		}

		if err := uc.audit(ctx, inv.GarageID, "staff.joined", "staff", member.ID, nil, member); err != nil {
			return err
		}

		return uc.Events.Publish(ctx, EventStaffJoined, inv.GarageID, accountID, map[string]any{
			keyGarageID: inv.GarageID, "account_id": accountID, "staff_id": member.ID,
		})
	})
	if err != nil {
		return nil, toAppError("accept invitation", err)
	}

	return inv, nil
}

func (uc *TenantUseCase) joinGarage(ctx context.Context, inv *domain.Invitation) (*domain.Staff, error) {
	member, err := uc.Staff.FindByAccount(ctx, inv.GarageID, inv.AccountID)

	switch {
	case errors.Is(err, domain.ErrStaffNotFound):
		member = &domain.Staff{GarageID: inv.GarageID, AccountID: inv.AccountID, Status: domain.StaffStatusActive}
		if err := uc.Staff.Create(ctx, member); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		member.Status = domain.StaffStatusActive
		if err := uc.Staff.UpdateStatus(ctx, member); err != nil {
			return nil, err
		}
	}

	groups, err := uc.Groups.List(ctx, inv.GarageID)
	if err != nil {
		return nil, err
	}

	var groupIDs []string

	for _, id := range inv.GroupIDs {
		for _, g := range groups {
			if g.ID == id {
				groupIDs = append(groupIDs, id)
			}
		}
	}

	return member, uc.Staff.SetGroups(ctx, inv.GarageID, member.ID, groupIDs)
}

// DeclineInvitation turns an invitation down.
func (uc *TenantUseCase) DeclineInvitation(ctx context.Context, accountID, invitationID string) (*domain.Invitation, error) {
	inv, err := uc.GetMyInvitation(ctx, accountID, invitationID)
	if err != nil {
		return nil, err
	}

	if err := inv.Decline(uc.now()); err != nil {
		return nil, toAppError("decline invitation", err)
	}

	if err := uc.Invitations.Update(ctx, inv); err != nil {
		return nil, toAppError("decline invitation", err)
	}

	return inv, nil
}

func (uc *TenantUseCase) hasPendingInvitation(ctx context.Context, garageID, accountID string) (bool, error) {
	invitations, err := uc.Invitations.ListByGarage(ctx, garageID)
	if err != nil {
		return false, toAppError("list invitations", err)
	}

	now := uc.now()

	for _, inv := range invitations {
		if inv.AccountID == accountID && inv.EffectiveStatus(now) == domain.InvitationPending {
			return true, nil
		}
	}

	return false, nil
}

// filterInvitations resolves effective statuses and keeps status (all when
// empty).
func (uc *TenantUseCase) filterInvitations(invitations []*domain.Invitation, status domain.InvitationStatus) []*domain.Invitation {
	now := uc.now()
	result := make([]*domain.Invitation, 0, len(invitations))

	for _, inv := range invitations {
		inv.Status = inv.EffectiveStatus(now)
		if status == "" || inv.Status == status {
			result = append(result, inv)
		}
	}

	return result
}

// withInvitationAccounts loads invitee and inviter names from the Identity
// module.
func (uc *TenantUseCase) withInvitationAccounts(ctx context.Context, invitations ...*domain.Invitation) error {
	ids := make([]string, 0, 2*len(invitations))
	for _, inv := range invitations {
		ids = append(ids, inv.AccountID, inv.InvitedBy)
	}

	accounts, err := uc.Accounts.ListByIDs(ctx, ids)
	if err != nil {
		return toAppError("load invitation accounts", err)
	}

	for _, inv := range invitations {
		if acc, ok := accounts[inv.AccountID]; ok {
			inv.FullName, inv.Email = acc.FullName, acc.Email
		}

		if acc, ok := accounts[inv.InvitedBy]; ok {
			inv.InvitedByName = acc.FullName
		}
	}

	return nil
}
