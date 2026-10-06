package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestInvitationFlow(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 1})
	id := e.garage(t)
	ctx := e.as(t, owner, id)
	advisor := groupNamed(t, e, id, "Service Advisor")

	candidate, err := e.UC.LookupCandidate(ctx, id, "LAN@example.com")
	require.NoError(t, err)
	assert.Equal(t, usecase.CandidateNone, candidate.Membership)

	candidate, err = e.UC.LookupCandidate(ctx, id, "0901234567")
	require.NoError(t, err)
	assert.Equal(t, staff, candidate.Account.ID)

	inv, err := e.UC.CreateInvitation(ctx, id, staff, []string{advisor.ID, advisor.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{advisor.ID}, inv.GroupIDs)
	assert.Equal(t, "Minh", inv.InvitedByName)
	assert.Equal(t, e.Now.Add(domain.InvitationTTL), inv.ExpiresAt)

	require.Len(t, e.Mail.Sent, 1)
	assert.Contains(t, e.Mail.Sent[0].Text, "https://vietgara.viettechno.com/invitations/"+inv.ID)
	assert.Contains(t, e.Mail.Sent[0].Subject, "Gara Minh", "invitee's language (vi)")

	candidate, err = e.UC.LookupCandidate(ctx, id, "lan@example.com")
	require.NoError(t, err)
	assert.Equal(t, usecase.CandidateInvited, candidate.Membership)

	_, err = e.UC.CreateInvitation(ctx, id, staff, nil)
	requireReason(t, err, "PLAN_LIMIT_REACHED")

	pending, err := e.UC.ListInvitations(ctx, id, domain.InvitationPending)
	require.NoError(t, err)
	assert.Len(t, pending, 1)

	staffCtx := e.as(t, staff, "")

	mine, err := e.UC.ListMyInvitations(staffCtx, staff, "")
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "Gara Minh", mine[0].GarageName)

	_, err = e.UC.GetMyInvitation(staffCtx, owner, inv.ID)
	requireReason(t, err, apperror.ReasonNotFound)

	accepted, err := e.UC.AcceptInvitation(staffCtx, staff, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationAccepted, accepted.Status)
	assert.Contains(t, e.Events.Types, usecase.EventStaffJoined)

	_, err = e.UC.AcceptInvitation(staffCtx, staff, inv.ID)
	requireReason(t, err, domain.ReasonInvitationNotPending)

	members, err := e.UC.ListStaff(ctx, id)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Contains(t, members[1].Permissions, access.CustomerWrite)

	candidate, err = e.UC.LookupCandidate(ctx, id, "lan@example.com")
	require.NoError(t, err)
	assert.Equal(t, usecase.CandidateMember, candidate.Membership)

	_, err = e.UC.CreateInvitation(ctx, id, staff, nil)
	requireReason(t, err, domain.ReasonAlreadyMember)
}

func TestInvitationValidation(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	_, err := e.UC.CreateInvitation(ctx, id, "ghost", nil)
	requireReason(t, err, domain.ReasonAccountNotFound)
	_, err = e.UC.CreateInvitation(ctx, id, "off", nil)
	requireReason(t, err, domain.ReasonAccountInactive)
	_, err = e.UC.CreateInvitation(ctx, id, staff, []string{"not-a-group"})
	requireReason(t, err, apperror.ReasonValidation)

	_, err = e.UC.LookupCandidate(ctx, id, "ghost@example.com")
	requireReason(t, err, domain.ReasonAccountNotFound)
	_, err = e.UC.LookupCandidate(ctx, id, "off@example.com")
	requireReason(t, err, domain.ReasonAccountInactive)

	inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
	require.NoError(t, err)

	_, err = e.UC.CreateInvitation(ctx, id, staff, nil)
	requireReason(t, err, domain.ReasonAlreadyInvited)

	revoked, err := e.UC.RevokeInvitation(ctx, id, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationRevoked, revoked.Status)

	_, err = e.UC.RevokeInvitation(ctx, "other-garage", inv.ID)
	requireReason(t, err, apperror.ReasonNotFound)
	_, err = e.UC.RevokeInvitation(ctx, id, inv.ID)
	requireReason(t, err, domain.ReasonInvitationNotPending)

	second, err := e.UC.CreateInvitation(ctx, id, staff, nil)
	require.NoError(t, err)

	declined, err := e.UC.DeclineInvitation(e.as(t, staff, ""), staff, second.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationDeclined, declined.Status)
	_, err = e.UC.DeclineInvitation(e.as(t, staff, ""), staff, second.ID)
	requireReason(t, err, domain.ReasonInvitationNotPending)
	_, err = e.UC.DeclineInvitation(e.as(t, staff, ""), staff, "missing")
	requireReason(t, err, apperror.ReasonNotFound)

	third, err := e.UC.CreateInvitation(ctx, id, staff, nil)
	require.NoError(t, err)

	e.Now = e.Now.Add(domain.InvitationTTL + time.Hour)

	expired, err := e.UC.ListMyInvitations(e.as(t, staff, ""), staff, domain.InvitationExpired)
	require.NoError(t, err)
	assert.Len(t, expired, 1)

	_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, third.ID)
	requireReason(t, err, domain.ReasonInvitationExpired)
}

func TestStaffManagement(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 1})
	id := e.garage(t)
	ctx := e.as(t, owner, id)
	accountant := groupNamed(t, e, id, "Accountant")

	inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
	require.NoError(t, err)
	_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
	require.NoError(t, err)

	members, err := e.UC.ListStaff(ctx, id)
	require.NoError(t, err)

	ownerRow, member := members[0], members[1]

	updated, err := e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{GroupIDs: &[]string{accountant.ID}})
	require.NoError(t, err)
	assert.Equal(t, []string{accountant.ID}, updated.GroupIDs)
	assert.Contains(t, updated.Permissions, access.BillingWrite)

	updated, err = e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{Status: new(domain.StaffStatusInactive)})
	require.NoError(t, err)
	assert.Equal(t, domain.StaffStatusInactive, updated.Status)

	_, err = e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{Status: new(domain.StaffStatus("gone"))})
	requireReason(t, err, apperror.ReasonValidation)

	_, err = e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{Status: new(domain.StaffStatusActive)})
	require.NoError(t, err, "one staff seat is free again")

	_, err = e.UC.UpdateStaff(ctx, id, ownerRow.ID, usecase.StaffFields{Status: new(domain.StaffStatusInactive)})
	requireReason(t, err, domain.ReasonOwnerMembership)
	requireReason(t, e.UC.DeleteStaff(ctx, id, ownerRow.ID), domain.ReasonOwnerMembership)

	_, err = e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{GroupIDs: &[]string{"bogus"}})
	requireReason(t, err, apperror.ReasonValidation)

	require.NoError(t, e.UC.DeleteStaff(ctx, id, member.ID))
	assert.Contains(t, e.Events.Types, usecase.EventStaffRemoved)
	requireReason(t, e.UC.DeleteStaff(ctx, id, member.ID), apperror.ReasonNotFound)
	_, err = e.UC.UpdateStaff(ctx, id, member.ID, usecase.StaffFields{})
	requireReason(t, err, apperror.ReasonNotFound)
}

func TestStaffFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
	require.NoError(t, err)
	_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
	require.NoError(t, err)

	members, err := e.UC.ListStaff(ctx, id)
	require.NoError(t, err)

	memberID := members[1].ID

	for op, call := range map[string]func() error{
		"staff.list": func() error { _, err := e.UC.ListStaff(ctx, id); return err },
		"staff.update": func() error {
			_, err := e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{Status: new(domain.StaffStatusInactive)})
			return err
		},
		"staff.groups": func() error {
			_, err := e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{GroupIDs: &[]string{}})
			return err
		},
		"staff.delete": func() error { return e.UC.DeleteStaff(ctx, id, memberID) },
		"group.list": func() error {
			_, err := e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{GroupIDs: &[]string{}})
			return err
		},
		"invitation.list": func() error {
			_, err := e.UC.ListInvitations(ctx, id, "")
			return err
		},
		"invitation.expire": func() error { _, err := e.UC.CreateInvitation(ctx, id, "off2", nil); return err },
		"staff.findByAccount": func() error {
			_, err := e.UC.LookupCandidate(ctx, id, "lan@example.com")
			return err
		},
	} {
		e.Store.Fail[op] = errBoom
		if op == "invitation.expire" {
			e.Dir.Accounts["off2"] = &usecase.AccountSummary{ID: "off2", Active: true}
		}

		require.Error(t, call(), op)
		delete(e.Store.Fail, op)
	}

	e.Dir.Err = errBoom
	_, err = e.UC.ListStaff(ctx, id)
	requireReason(t, err, apperror.ReasonInternal)
	_, err = e.UC.ListMyInvitations(ctx, staff, "")
	requireReason(t, err, apperror.ReasonInternal)
}
