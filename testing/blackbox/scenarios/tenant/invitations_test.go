package tenant_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-TEN-007 (partial): an existing user is looked up, invited
// by e-mail, and joins through acceptance with the invited groups; the
// invitation e-mail names inviter, garage and link. Badges, link clicks
// and copy are web-E2E.
func TestInviteByEmailAndAccept(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Invite"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	invitee := fixture.NewVerifiedOwnerWithLocale(t, "invitee7", identityv1.Locale_LOCALE_EN)
	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), invitee.Email)
	require.NoError(t, err)
	assert.Equal(t, invitee.Session.GetUser().GetId(), cand.GetUserId())
	assert.Equal(t, tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_NONE, cand.GetMembership())

	inv, err := owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups["Service Advisor"]})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, inv.GetStatus())
	assert.Contains(t, inv.GetGroupIds(), groups["Service Advisor"])
	assert.WithinRange(t, inv.GetExpiresAt().AsTime(),
		time.Now().Add(6*24*time.Hour), time.Now().Add(8*24*time.Hour))

	// The invitee already has their sign-up e-mails (the welcome one can
	// still be arriving), so wait for the invitation itself: the e-mail
	// with its link.
	msg, err := mail.WaitForMatching(ctx, invitee.Email, func(m *client.Message) bool {
		return strings.Contains(m.Text, "/invitations/"+inv.GetId())
	})
	require.NoError(t, err)
	assert.Contains(t, msg.Subject, owner.Session.GetUser().GetFullName())
	assert.Contains(t, msg.Subject, garage.GetName())

	listed, err := owner.API.ListInvitations(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING,
		invitationByID(t, listed, inv.GetId()).GetStatus())

	mine, err := invitee.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	invitationByID(t, mine, inv.GetId())

	accepted, err := invitee.API.AcceptInvitation(ctx, inv.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_ACCEPTED, accepted.GetStatus())

	staff := staffByUserID(t, owner.API, garage.GetId(), invitee.Session.GetUser().GetId())
	assert.Contains(t, staff.GetGroupIds(), groups["Service Advisor"])

	phone := "0900000921"
	_, err = invitee.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "Khách Mời", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)
}

// Scenario: E2E-TEN-008 (partial): inviting by phone number finds the user
// and joins with the invited group; a Technician sees no customer,
// partner or staff surface. Sidebar details are web-E2E.
func TestInviteByPhone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter-phone", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Phone Invite"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	phone := "0900000922"
	registered, err := fixture.Anonymous(t).Register(ctx, &identityv1.RegisterRequest{
		Email: fixture.UniqueEmail(t, "invitee-phone"), Password: fixture.Password,
		FullName: "Blackbox Phone", Phone: &phone,
	})
	require.NoError(t, err)
	inviteeMail := fixture.Mailpit(t)
	inviteMsg, err := inviteeMail.WaitForMessage(ctx, registered.GetUser().GetEmail())
	require.NoError(t, err)
	inviteOTP, err := inviteMsg.OTP()
	require.NoError(t, err)
	verified, err := fixture.Anonymous(t).WithToken(registered.GetAccessToken()).
		VerifyEmail(ctx, inviteOTP, registered.GetRefreshToken())
	require.NoError(t, err)
	inviteeAPI := fixture.Anonymous(t).WithToken(verified.GetAccessToken())

	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), phone)
	require.NoError(t, err)
	assert.Equal(t, verified.GetUser().GetId(), cand.GetUserId())

	inv, err := owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups["Technician"]})
	require.NoError(t, err)

	_, err = inviteeAPI.AcceptInvitation(ctx, inv.GetId())
	require.NoError(t, err)

	_, err = inviteeAPI.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	_, err = inviteeAPI.ListStaff(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")
}

// Scenario: E2E-TEN-009 (partial): invitation lookups distinguish unknown
// users, members, pending invitees and the owner; creating duplicates is
// refused. The find-dialog notices are web-E2E.
func TestInvitationLookupFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter-lookup", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Lookup"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	member := inviteMember(t, owner.API, garage.GetId(), []string{groups["Service Advisor"]}, "lookup-member")
	pending, pendingInv := invitePending(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "lookup-pending")
	_ = pendingInv

	_, err = owner.API.LookupStaffCandidate(ctx, garage.GetId(), "nobody@vietgara.test")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "USER_NOT_FOUND")

	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), member.Email)
	require.NoError(t, err)
	assert.Equal(t, tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_MEMBER, cand.GetMembership())

	cand, err = owner.API.LookupStaffCandidate(ctx, garage.GetId(), pending.Email)
	require.NoError(t, err)
	assert.Equal(t, tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_INVITED, cand.GetMembership())

	cand, err = owner.API.LookupStaffCandidate(ctx, garage.GetId(), owner.Email)
	require.NoError(t, err)
	assert.NotEqual(t, tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_NONE, cand.GetMembership())

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), member.Session.GetUser().GetId(), []string{groups["Technician"]})
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_MEMBER")

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), pending.Session.GetUser().GetId(), []string{groups["Technician"]})
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_INVITED")

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), owner.Session.GetUser().GetId(), []string{groups["Technician"]})
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_MEMBER")
}

// Scenario: E2E-TEN-010 (partial): declining removes the invitation from
// the pending list without joining, and a new invitation stays possible.
func TestDeclineInvitation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter-decline", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Decline"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	invitee, inv := invitePending(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "decliner")

	declined, err := invitee.API.DeclineInvitation(ctx, inv.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_DECLINED, declined.GetStatus())

	mine, err := invitee.API.ListMyInvitations(ctx)
	require.NoError(t, err)

	for _, m := range mine {
		if m.GetId() == inv.GetId() {
			assert.NotEqual(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, m.GetStatus())
		}
	}

	listed, err := owner.API.ListInvitations(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_DECLINED,
		invitationByID(t, listed, inv.GetId()).GetStatus())

	again, err := owner.API.CreateInvitation(ctx, garage.GetId(), invitee.Session.GetUser().GetId(), []string{groups["Technician"]})
	require.NoError(t, err)
	assert.NotEqual(t, inv.GetId(), again.GetId())
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, again.GetStatus())
}

// Scenario: E2E-TEN-011 (partial): revoking a pending invitation blocks
// acceptance afterwards.
func TestRevokeInvitation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter-revoke", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Revoke"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	invitee, inv := invitePending(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "revokee")

	revoked, err := owner.API.RevokeInvitation(ctx, garage.GetId(), inv.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_REVOKED, revoked.GetStatus())

	_, err = invitee.API.AcceptInvitation(ctx, inv.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "INVITATION_NOT_PENDING")

	mine, err := invitee.API.ListMyInvitations(ctx)
	require.NoError(t, err)

	for _, m := range mine {
		if m.GetId() == inv.GetId() {
			assert.NotEqual(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, m.GetStatus())
		}
	}
}

// Scenario: E2E-TEN-013 (partial): an invitation opened by another user
// cannot be accepted, and stays pending for its addressee.
func TestInvitationWrongUser(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inviter-wrong-user", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Wrong User"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	invitee, inv := invitePending(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "invitee13")
	other := fixture.NewVerifiedOwnerWithLocale(t, "other13", identityv1.Locale_LOCALE_EN)

	_, err = other.API.AcceptInvitation(ctx, inv.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	listed, err := owner.API.ListInvitations(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING,
		invitationByID(t, listed, inv.GetId()).GetStatus())

	_, err = owner.API.RevokeInvitation(ctx, garage.GetId(), inv.GetId())
	require.NoError(t, err)

	_ = invitee
}
