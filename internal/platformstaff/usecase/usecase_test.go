package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/platformstafftest"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	adminID = "u-admin"
	staffID = "u-staff"
	billID  = "u-bill"
)

type env struct {
	uc    *usecase.UseCase
	repo  *platformstafftest.Repo
	users *platformstafftest.Users
	rec   *platformstafftest.Recorder
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: platformstafftest.NewRepo(), rec: &platformstafftest.Recorder{}, now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		users: &platformstafftest.Users{ByID: map[string]*usecase.UserSummary{
			adminID: {ID: adminID, FullName: "Admin One", Email: "admin@x.test", Locale: "vi", Active: true, EmailVerified: true},
			staffID: {ID: staffID, FullName: "Staff Manager", Email: "staff@x.test", Locale: "en", Active: true, EmailVerified: true},
			billID:  {ID: billID, FullName: "Bill Support", Email: "bill@x.test", Locale: "vi", Active: true, EmailVerified: true},
			"u-off": {ID: "u-off", FullName: "Inactive", Email: "off@x.test", Active: false, EmailVerified: true},
			"u-new": {ID: "u-new", FullName: "Unverified", Email: "new@x.test", Active: true},
		}},
	}
	e.repo.Admins = []string{adminID}
	e.uc = usecase.New(usecase.Deps{
		Repo: e.repo, Users: e.users, Audit: e.rec, Mailer: e.rec, AdminBaseURL: "https://admin.test/", Now: func() time.Time { return e.now },
	})

	return e
}

// as returns a context for userID with its platform access loaded.
func (e *env) as(t *testing.T, userID string) context.Context {
	t.Helper()

	acc, err := e.repo.PlatformAccess(t.Context(), userID)
	require.NoError(t, err)

	ctx := middleware.WithClaims(t.Context(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID}})

	return access.WithPlatformAccess(ctx, acc)
}

// as2 is as without the *testing.T, for use inside closures.
func (e *env) as2(userID string) context.Context {
	acc, _ := e.repo.PlatformAccess(context.Background(), userID)
	ctx := middleware.WithClaims(context.Background(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID}})

	return access.WithPlatformAccess(ctx, acc)
}

func requireCode(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "want an app error, got %v", err)
	assert.Equal(t, reason, appErr.ReasonOrDefault())
}

// seedBilling creates the role "Billing" (subscription.read) and the group
// "Billing support" and returns their ids.
func (e *env) seedBilling(t *testing.T) (roleID, groupID string) {
	t.Helper()

	ctx := e.as(t, adminID)

	role, err := e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("Billing"), Permissions: new([]access.PlatformPermission{access.PlatformSubscriptionRead})})
	require.NoError(t, err)

	group, err := e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("Billing support"), RoleID: &role.ID})
	require.NoError(t, err)

	return role.ID, group.ID
}

func TestRoleLifecycle(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(t, adminID)

	role, err := e.uc.CreateRole(ctx, usecase.RoleFields{
		Name: new("Billing"), Description: new("Reads subscriptions"),
		Permissions: new([]access.PlatformPermission{access.PlatformSubscriptionWrite, access.PlatformSubscriptionWrite}),
	})
	require.NoError(t, err)
	assert.Equal(t, []access.PlatformPermission{access.PlatformSubscriptionRead, access.PlatformSubscriptionWrite}, role.Permissions,
		"a write includes its read; duplicates are dropped")

	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("billing")})
	requireCode(t, err, codes.AlreadyExists, domain.ReasonRoleNameTaken)

	got, err := e.uc.GetRole(ctx, role.ID)
	require.NoError(t, err)
	assert.Equal(t, "Billing", got.Name)

	updated, err := e.uc.UpdateRole(ctx, role.ID, usecase.RoleFields{Permissions: new([]access.PlatformPermission{access.PlatformPlanRead})})
	require.NoError(t, err)
	assert.Equal(t, []access.PlatformPermission{access.PlatformPlanRead}, updated.Permissions)
	assert.Equal(t, "Billing", updated.Name, "fields not in the update are kept")

	list, err := e.uc.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	group, err := e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("G"), RoleID: &role.ID})
	require.NoError(t, err)

	err = e.uc.DeleteRole(ctx, role.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonRoleInUse)

	require.NoError(t, e.uc.DeleteGroup(ctx, group.ID))
	require.NoError(t, e.uc.DeleteRole(ctx, role.ID))

	_, err = e.uc.GetRole(ctx, role.ID)
	require.Error(t, err)

	assert.Equal(t, []string{
		"platform_role.created", "platform_role.updated", "platform_group.created", "platform_group.deleted", "platform_role.deleted",
	}, e.rec.Actions())
	assert.Equal(t, adminID, e.rec.Entries[0].Actor)
	assert.Empty(t, e.rec.Entries[0].GarageID, "platform entries belong to no garage")
}

func TestRoleValidation(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(t, adminID)

	_, err := e.uc.CreateRole(ctx, usecase.RoleFields{})
	require.Error(t, err, "a name is required")

	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("")})
	require.Error(t, err)

	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new(string(make([]rune, 101)))})
	require.Error(t, err)

	long := string(make([]rune, 501))
	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("x"), Description: &long})
	require.Error(t, err)

	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("x"), Permissions: new([]access.PlatformPermission{"plan.delete"})})
	require.Error(t, err, "unknown permissions are refused")

	_, err = e.uc.UpdateRole(ctx, "missing", usecase.RoleFields{})
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	require.Error(t, e.uc.DeleteRole(ctx, "missing"))
	assert.Empty(t, e.rec.Entries)
}

func TestOnlyHeldPermissionsCanBeGiven(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	_, billingGroup := e.seedBilling(t)

	// staff may manage platform staff and read subscriptions, nothing else.
	adminCtx := e.as(t, adminID)
	staffRole, err := e.uc.CreateRole(adminCtx, usecase.RoleFields{
		Name: new("Staff manager"), Permissions: new([]access.PlatformPermission{access.PlatformStaffWrite, access.PlatformSubscriptionRead}),
	})
	require.NoError(t, err)

	staffGroup, err := e.uc.CreateGroup(adminCtx, usecase.GroupFields{Name: new("Managers"), RoleID: &staffRole.ID})
	require.NoError(t, err)

	e.repo.Members[staffGroup.ID] = []string{staffID}
	ctx := e.as(t, staffID)

	_, err = e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("Sneaky"), Permissions: new([]access.PlatformPermission{access.PlatformPlanWrite})})
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	ok, err := e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("Reader"), Permissions: new([]access.PlatformPermission{access.PlatformSubscriptionRead})})
	require.NoError(t, err, "a permission the caller holds can be given")

	_, err = e.uc.UpdateRole(ctx, ok.ID, usecase.RoleFields{Permissions: new([]access.PlatformPermission{access.PlatformAuditRead})})
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	// A role with a permission the caller lacks cannot be edited, assigned or removed from.
	powerful, err := e.uc.CreateRole(adminCtx, usecase.RoleFields{Name: new("Power"), Permissions: new([]access.PlatformPermission{access.PlatformPlanWrite})})
	require.NoError(t, err)

	powerGroup, err := e.uc.CreateGroup(adminCtx, usecase.GroupFields{Name: new("Power group"), RoleID: &powerful.ID})
	require.NoError(t, err)

	e.repo.Members[powerGroup.ID] = []string{billID}

	_, err = e.uc.UpdateRole(ctx, powerful.ID, usecase.RoleFields{Name: new("Renamed")})
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("Mine"), RoleID: &powerful.ID})
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	_, err = e.uc.UpdateGroup(ctx, powerGroup.ID, usecase.GroupFields{Name: new("Renamed")})
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	requireCode(t, e.uc.DeleteGroup(ctx, powerGroup.ID), codes.PermissionDenied, domain.ReasonPermissionNotHeld)
	requireCode(t, e.uc.RemoveMember(ctx, powerGroup.ID, billID), codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	_, err = e.uc.CreateInvitation(ctx, "bill@x.test", powerGroup.ID)
	requireCode(t, err, codes.PermissionDenied, domain.ReasonPermissionNotHeld)

	// An invitation to a group with a role the caller holds is fine.
	_, err = e.uc.CreateInvitation(ctx, "off@x.test", billingGroup)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonUserInactive)

	// Without platform access in the context nothing can be given.
	_, err = e.uc.CreateRole(middleware.WithClaims(t.Context(), &auth.Claims{}), usecase.RoleFields{Name: new("z"), Permissions: new([]access.PlatformPermission{access.PlatformPlanRead})})
	require.Error(t, err)
}

func TestGroupLifecycle(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(t, adminID)
	roleID, groupID := e.seedBilling(t)

	_, err := e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("Billing support"), RoleID: &roleID})
	requireCode(t, err, codes.AlreadyExists, domain.ReasonGroupNameTaken)

	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{RoleID: &roleID})
	require.Error(t, err, "a name is required")

	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("x")})
	require.Error(t, err, "a role is required")

	missing := "role-missing"
	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("x"), RoleID: &missing})
	requireCode(t, err, codes.InvalidArgument, "VALIDATION_ERROR")

	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new(""), RoleID: &roleID})
	require.Error(t, err)

	long := string(make([]rune, 501))
	_, err = e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("x"), Description: &long, RoleID: &roleID})
	require.Error(t, err)

	other, err := e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("Plans"), Permissions: new([]access.PlatformPermission{access.PlatformPlanRead})})
	require.NoError(t, err)

	updated, err := e.uc.UpdateGroup(ctx, groupID, usecase.GroupFields{Description: new("Billing desk"), RoleID: &other.ID})
	require.NoError(t, err)
	assert.Equal(t, "Plans", updated.RoleName)
	assert.Equal(t, "Billing desk", updated.Description)

	groups, err := e.uc.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)

	_, _, err = e.uc.GetGroup(ctx, "missing")
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	_, err = e.uc.UpdateGroup(ctx, "missing", usecase.GroupFields{})
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	requireCode(t, e.uc.DeleteGroup(ctx, "missing"), codes.NotFound, "NOT_FOUND")
}

// TestBillingSupportFlow is the Definition of Done of Release 8.1: a group
// with a role that can only read subscriptions, a user added through an
// invitation, and a removal that applies at once.
func TestBillingSupportFlow(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	_, groupID := e.seedBilling(t)
	adminCtx := e.as(t, adminID)

	inv, err := e.uc.CreateInvitation(adminCtx, "BILL@x.test", groupID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationPending, inv.Status)
	assert.Equal(t, "Bill Support", inv.FullName)
	assert.Equal(t, "Admin One", inv.InvitedByName)
	assert.Equal(t, "Billing support", inv.GroupName)

	require.Len(t, e.rec.Sent, 1)
	assert.Equal(t, "bill@x.test", e.rec.Sent[0].ToEmail)
	assert.Contains(t, e.rec.Sent[0].Text, "https://admin.test/invitations/"+inv.ID)
	assert.Contains(t, e.rec.Sent[0].Text, "Billing support")

	// Before accepting, the user has no platform access but sees the invitation.
	billCtx := e.as(t, billID)
	acc, pending, err := e.uc.MyAccess(billCtx)
	require.NoError(t, err)
	assert.False(t, acc.Any())
	assert.Equal(t, 1, pending)

	mine, err := e.uc.ListMyInvitations(billCtx, domain.InvitationPending)
	require.NoError(t, err)
	require.Len(t, mine, 1)

	_, err = e.uc.AcceptInvitation(e.as(t, staffID), inv.ID)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	accepted, err := e.uc.AcceptInvitation(billCtx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationAccepted, accepted.Status)

	acc, pending, err = e.uc.MyAccess(e.as(t, billID))
	require.NoError(t, err)
	assert.Equal(t, []access.PlatformPermission{access.PlatformSubscriptionRead}, acc.Effective(),
		"subscriptions can be read, nothing can be edited and plans are out of reach")
	require.NoError(t, acc.Check(access.PlatformSubscriptionRead))
	require.Error(t, acc.Check(access.PlatformSubscriptionWrite))
	require.Error(t, acc.Check(access.PlatformPlanRead))
	assert.Zero(t, pending)

	_, err = e.uc.AcceptInvitation(billCtx, inv.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonInvitationNotPending)

	staff, err := e.uc.ListStaff(adminCtx)
	require.NoError(t, err)
	require.Len(t, staff, 2)
	assert.True(t, staff[0].Admin, "Platform Admins come first")
	assert.Equal(t, "Bill Support", staff[1].FullName)
	assert.Equal(t, []domain.GroupRef{{ID: groupID, Name: "Billing support"}}, staff[1].Groups)

	group, members, err := e.uc.GetGroup(adminCtx, groupID)
	require.NoError(t, err)
	assert.Equal(t, 1, group.MemberCount)
	require.Len(t, members, 1)
	assert.Equal(t, "bill@x.test", members[0].Email)

	require.NoError(t, e.uc.RemoveMember(adminCtx, groupID, billID))
	acc, _, err = e.uc.MyAccess(e.as(t, billID))
	require.NoError(t, err)
	assert.False(t, acc.Any(), "removing the user blocks the next request")

	requireCode(t, e.uc.RemoveMember(adminCtx, groupID, billID), codes.NotFound, "NOT_FOUND")

	assert.Equal(t, []string{
		"platform_role.created", "platform_group.created", "platform_staff.invited", "platform_staff.joined", "platform_staff.removed",
	}, e.rec.Actions())
}

func TestInvitationRules(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	_, groupID := e.seedBilling(t)
	ctx := e.as(t, adminID)

	_, err := e.uc.CreateInvitation(ctx, "nobody@x.test", groupID)
	requireCode(t, err, codes.NotFound, domain.ReasonUserNotFound)

	_, err = e.uc.CreateInvitation(ctx, "off@x.test", groupID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonUserInactive)

	_, err = e.uc.CreateInvitation(ctx, "new@x.test", groupID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonUserInactive)

	_, err = e.uc.CreateInvitation(ctx, "admin@x.test", groupID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonCannotInviteSelf)

	e.repo.Admins = append(e.repo.Admins, staffID)
	_, err = e.uc.CreateInvitation(ctx, "staff@x.test", groupID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonAlreadyAdmin)

	_, err = e.uc.CreateInvitation(ctx, "bill@x.test", "missing")
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	first, err := e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	require.NoError(t, err)

	_, err = e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	requireCode(t, err, codes.AlreadyExists, domain.ReasonAlreadyInvited)

	// Revoking frees the user for a new invitation.
	revoked, err := e.uc.RevokeInvitation(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationRevoked, revoked.Status)

	_, err = e.uc.RevokeInvitation(ctx, first.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonInvitationNotPending)

	_, err = e.uc.RevokeInvitation(ctx, "missing")
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	second, err := e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	require.NoError(t, err)

	// An invitation that expired cannot be accepted, and a new one can be made.
	e.now = e.now.Add(domain.InvitationTTL + time.Hour)

	_, err = e.uc.AcceptInvitation(e.as(t, billID), second.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonInvitationExpired)

	expired, err := e.uc.ListInvitations(ctx, domain.InvitationExpired)
	require.NoError(t, err)
	require.Len(t, expired, 1)

	third, err := e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	require.NoError(t, err)

	declined, err := e.uc.DeclineInvitation(e.as(t, billID), third.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationDeclined, declined.Status)

	_, err = e.uc.DeclineInvitation(e.as(t, billID), third.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonInvitationNotPending)

	all, err := e.uc.ListInvitations(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	pending, err := e.uc.ListInvitations(ctx, domain.InvitationPending)
	require.NoError(t, err)
	assert.Empty(t, pending)

	// A member cannot be invited again.
	e.repo.Members[groupID] = []string{billID}
	_, err = e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	requireCode(t, err, codes.AlreadyExists, domain.ReasonAlreadyMember)
}

func TestDeletingAGroupRemovesAccessAndInvitations(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	_, groupID := e.seedBilling(t)
	ctx := e.as(t, adminID)

	inv, err := e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	require.NoError(t, err)

	_, err = e.uc.AcceptInvitation(e.as(t, billID), inv.ID)
	require.NoError(t, err)

	pendingInv, err := e.uc.CreateInvitation(ctx, "staff@x.test", groupID)
	require.NoError(t, err)

	require.NoError(t, e.uc.DeleteGroup(ctx, groupID))

	acc, _, err := e.uc.MyAccess(e.as(t, billID))
	require.NoError(t, err)
	assert.False(t, acc.Any())

	_, err = e.uc.AcceptInvitation(e.as(t, staffID), pendingInv.ID)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")
}

func TestStaffListsAdminsAndSurvivesMissingUsers(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(t, adminID)
	_, groupID := e.seedBilling(t)

	e.repo.Members[groupID] = []string{billID, "u-gone"}

	staff, err := e.uc.ListStaff(ctx)
	require.NoError(t, err)
	require.Len(t, staff, 3)
	assert.Equal(t, adminID, staff[0].UserID)
}

func TestFailuresAreInternalErrors(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		"ListRoles", "FindRole", "CreateRole", "UpdateRole", "DeleteRole", "ListGroups", "FindGroup", "CreateGroup", "UpdateGroup",
		"DeleteGroup", "GroupMembers", "Memberships", "AdminIDs", "IsAdmin", "AddMember", "RemoveMember", "IsMember",
		"CreateInvitation", "FindInvitation", "UpdateInvitation", "ListInvitations", "ListInvitationsByUser", "ExpireStale", "PlatformAccess",
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			roleID, groupID := e.seedBilling(t)
			ctx := e.as(t, adminID)

			e.repo.Members[groupID] = []string{billID}
			e.repo.Invitations["inv-x"] = &domain.Invitation{
				ID: "inv-x", GroupID: groupID, UserID: staffID, InvitedBy: adminID, Status: domain.InvitationPending, ExpiresAt: e.now.Add(time.Hour),
			}
			e.repo.FailOn = method

			calls := map[string]func() error{
				"ListRoles":  func() error { _, err := e.uc.ListRoles(ctx); return err },
				"FindRole":   func() error { _, err := e.uc.GetRole(ctx, roleID); return err },
				"CreateRole": func() error { _, err := e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("r")}); return err },
				"UpdateRole": func() error { _, err := e.uc.UpdateRole(ctx, roleID, usecase.RoleFields{Name: new("r")}); return err },
				"DeleteRole": func() error { return e.uc.DeleteRole(ctx, roleID) },
				"ListGroups": func() error { _, err := e.uc.ListGroups(ctx); return err },
				"FindGroup":  func() error { _, _, err := e.uc.GetGroup(ctx, groupID); return err },
				"CreateGroup": func() error {
					_, err := e.uc.CreateGroup(ctx, usecase.GroupFields{Name: new("g"), RoleID: &roleID})
					return err
				},
				"UpdateGroup": func() error {
					_, err := e.uc.UpdateGroup(ctx, groupID, usecase.GroupFields{Name: new("g")})
					return err
				},
				"DeleteGroup":           func() error { return e.uc.DeleteGroup(ctx, groupID) },
				"GroupMembers":          func() error { _, _, err := e.uc.GetGroup(ctx, groupID); return err },
				"Memberships":           func() error { _, err := e.uc.ListStaff(ctx); return err },
				"AdminIDs":              func() error { _, err := e.uc.ListStaff(ctx); return err },
				"IsAdmin":               func() error { _, err := e.uc.CreateInvitation(ctx, "staff@x.test", groupID); return err },
				"AddMember":             func() error { _, err := e.uc.AcceptInvitation(e.as2(staffID), "inv-x"); return err },
				"RemoveMember":          func() error { return e.uc.RemoveMember(ctx, groupID, billID) },
				"IsMember":              func() error { _, err := e.uc.CreateInvitation(ctx, "staff@x.test", groupID); return err },
				"CreateInvitation":      func() error { _, err := e.uc.CreateInvitation(ctx, "staff2@x.test", groupID); return err },
				"FindInvitation":        func() error { _, err := e.uc.RevokeInvitation(ctx, "inv-x"); return err },
				"UpdateInvitation":      func() error { _, err := e.uc.RevokeInvitation(ctx, "inv-x"); return err },
				"ListInvitations":       func() error { _, err := e.uc.ListInvitations(ctx, ""); return err },
				"ListInvitationsByUser": func() error { _, err := e.uc.ListMyInvitations(ctx, ""); return err },
				"ExpireStale":           func() error { _, err := e.uc.CreateInvitation(ctx, "staff@x.test", groupID); return err },
				"PlatformAccess":        func() error { _, _, err := e.uc.MyAccess(ctx); return err },
			}

			// A method that fails early may mask the one under test; the
			// scenario only needs to end in an internal error.
			e.users.ByID["staff2@x.test"] = &usecase.UserSummary{ID: "u-s2", Email: "staff2@x.test", Active: true, EmailVerified: true}

			err := calls[method]()
			require.Error(t, err)

			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, apperror.CodeInternal, appErr.Code)
			assert.NotEmpty(t, appErr.Error())
		})
	}
}

func TestAuditFailureFailsTheChange(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.rec.FailAudit = true
	ctx := e.as(t, adminID)

	_, err := e.uc.CreateRole(ctx, usecase.RoleFields{Name: new("r")})
	require.Error(t, err, "a change whose audit entry cannot be written fails")
}

func TestUserDirectoryFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(t, adminID)
	_, groupID := e.seedBilling(t)

	inv, err := e.uc.CreateInvitation(ctx, "bill@x.test", groupID)
	require.NoError(t, err)

	e.users.Fails = true

	_, err = e.uc.ListInvitations(ctx, "")
	require.Error(t, err)

	_, err = e.uc.ListMyInvitations(e.as(t, billID), "")
	require.Error(t, err)

	_, _, err = e.uc.GetGroup(ctx, groupID)
	require.Error(t, err)

	_, err = e.uc.ListStaff(ctx)
	require.Error(t, err)

	_, err = e.uc.AcceptInvitation(e.as(t, billID), inv.ID)
	require.Error(t, err)
}

func TestCatalogue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, access.PlatformCatalog, newEnv(t).uc.PermissionCatalogue())
}
