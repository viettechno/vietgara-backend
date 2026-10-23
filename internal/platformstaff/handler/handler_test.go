package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/handler"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/platformstafftest"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
)

type fixture struct {
	srv  *handler.Server
	repo *platformstafftest.Repo
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	repo := platformstafftest.NewRepo()
	repo.Admins = []string{"admin"}
	users := &platformstafftest.Users{ByID: map[string]*usecase.UserSummary{
		"admin": {ID: "admin", FullName: "Admin", Email: "admin@x.test", Active: true, EmailVerified: true},
		"bill":  {ID: "bill", FullName: "Bill", Email: "bill@x.test", Locale: "en", Active: true, EmailVerified: true},
	}}
	rec := &platformstafftest.Recorder{}
	uc := usecase.New(usecase.Deps{
		Repo: repo, Users: users, Audit: rec, Mailer: rec, AdminBaseURL: "https://admin.test",
		Now: func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) },
	})

	return &fixture{srv: handler.NewServer(uc), repo: repo}
}

func (f *fixture) as(t *testing.T, userID string) context.Context {
	t.Helper()

	acc, err := f.repo.PlatformAccess(t.Context(), userID)
	require.NoError(t, err)

	ctx := middleware.WithClaims(t.Context(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID}})

	return access.WithPlatformAccess(ctx, acc)
}

func requireStatus(t *testing.T, err error, code codes.Code) {
	t.Helper()

	require.Error(t, err)
	assert.Equal(t, code, status.Code(err))
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()

	assert.Equal(t, access.PlatformStaffWrite, p[platformv1.PlatformStaffService_CreatePlatformRole_FullMethodName].Platform)
	assert.Equal(t, access.PlatformStaffRead, p[platformv1.PlatformStaffService_ListPlatformStaff_FullMethodName].Platform)
	assert.Equal(t, access.PlatformStaffWrite, p[platformv1.PlatformStaffService_CreatePlatformInvitation_FullMethodName].Platform)

	for _, method := range []string{
		platformv1.PlatformAccessService_GetMyPlatformAccess_FullMethodName,
		platformv1.PlatformAccessService_AcceptPlatformInvitation_FullMethodName,
	} {
		assert.Empty(t, p[method].Platform, "%s needs no platform permission: an invitee has none yet", method)
		assert.False(t, p[method].Public)
	}

	assert.Len(t, p, 20)
}

func TestRolesAndGroupsThroughTheAPI(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := f.as(t, "admin")

	catalogue, err := f.srv.ListPlatformPermissions(ctx, &platformv1.ListPlatformPermissionsRequest{})
	require.NoError(t, err)
	require.Len(t, catalogue.GetData(), 12)
	assert.Equal(t, platformv1.PlatformPermission_PLATFORM_PERMISSION_PLAN_READ, catalogue.GetData()[0].GetPermission())
	assert.Equal(t, "plan", catalogue.GetData()[0].GetModule())
	assert.Equal(t, "read", catalogue.GetData()[0].GetAction())

	created, err := f.srv.CreatePlatformRole(ctx, &platformv1.CreatePlatformRoleRequest{Role: &platformv1.PlatformRole{
		Name: "Billing", Description: "d",
		Permissions: []platformv1.PlatformPermission{platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_WRITE},
	}})
	require.NoError(t, err)

	role := created.GetRole()
	assert.Equal(t, []platformv1.PlatformPermission{
		platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_READ,
		platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_WRITE,
	}, role.GetPermissions())

	_, err = f.srv.CreatePlatformRole(ctx, &platformv1.CreatePlatformRoleRequest{Role: &platformv1.PlatformRole{
		Name: "Bad", Permissions: []platformv1.PlatformPermission{platformv1.PlatformPermission_PLATFORM_PERMISSION_UNSPECIFIED},
	}})
	requireStatus(t, err, codes.InvalidArgument)

	updated, err := f.srv.UpdatePlatformRole(ctx, &platformv1.UpdatePlatformRoleRequest{
		RoleId: role.GetId(), Role: &platformv1.PlatformRole{Name: "Billing desk"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Billing desk", updated.GetRole().GetName())
	assert.Len(t, updated.GetRole().GetPermissions(), 2, "a masked update leaves the other fields")

	_, err = f.srv.UpdatePlatformRole(ctx, &platformv1.UpdatePlatformRoleRequest{
		RoleId: role.GetId(), Role: &platformv1.PlatformRole{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"groupCount"}},
	})
	requireStatus(t, err, codes.InvalidArgument)

	got, err := f.srv.GetPlatformRole(ctx, &platformv1.GetPlatformRoleRequest{RoleId: role.GetId()})
	require.NoError(t, err)
	assert.Equal(t, "Billing desk", got.GetRole().GetName())

	roles, err := f.srv.ListPlatformRoles(ctx, &platformv1.ListPlatformRolesRequest{})
	require.NoError(t, err)
	assert.Len(t, roles.GetData(), 1)

	group, err := f.srv.CreatePlatformGroup(ctx, &platformv1.CreatePlatformGroupRequest{Group: &platformv1.PlatformStaffGroup{
		Name: "Billing support", Description: "d", RoleId: role.GetId(),
	}})
	require.NoError(t, err)
	assert.Equal(t, "Billing desk", group.GetGroup().GetRoleName())

	renamed, err := f.srv.UpdatePlatformGroup(ctx, &platformv1.UpdatePlatformGroupRequest{
		GroupId: group.GetGroup().GetId(), Group: &platformv1.PlatformStaffGroup{Name: "Desk"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Desk", renamed.GetGroup().GetName())

	byJSONName, err := f.srv.UpdatePlatformGroup(ctx, &platformv1.UpdatePlatformGroupRequest{
		GroupId: group.GetGroup().GetId(), Group: &platformv1.PlatformStaffGroup{RoleId: role.GetId()},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"roleId"}},
	})
	require.NoError(t, err)
	assert.Equal(t, role.GetId(), byJSONName.GetGroup().GetRoleId())

	_, err = f.srv.UpdatePlatformGroup(ctx, &platformv1.UpdatePlatformGroupRequest{
		GroupId: group.GetGroup().GetId(), Group: &platformv1.PlatformStaffGroup{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"memberCount"}},
	})
	requireStatus(t, err, codes.InvalidArgument)

	groups, err := f.srv.ListPlatformGroups(ctx, &platformv1.ListPlatformGroupsRequest{})
	require.NoError(t, err)
	require.Len(t, groups.GetData(), 1)

	detail, err := f.srv.GetPlatformGroup(ctx, &platformv1.GetPlatformGroupRequest{GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)
	assert.Empty(t, detail.GetMembers())

	_, err = f.srv.DeletePlatformRole(ctx, &platformv1.DeletePlatformRoleRequest{RoleId: role.GetId()})
	requireStatus(t, err, codes.FailedPrecondition)

	_, err = f.srv.DeletePlatformGroup(ctx, &platformv1.DeletePlatformGroupRequest{GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)

	_, err = f.srv.DeletePlatformRole(ctx, &platformv1.DeletePlatformRoleRequest{RoleId: role.GetId()})
	require.NoError(t, err)

	_, err = f.srv.GetPlatformRole(ctx, &platformv1.GetPlatformRoleRequest{RoleId: role.GetId()})
	requireStatus(t, err, codes.NotFound)
}

func TestInvitationFlowThroughTheAPI(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	admin := f.as(t, "admin")

	role, err := f.srv.CreatePlatformRole(admin, &platformv1.CreatePlatformRoleRequest{Role: &platformv1.PlatformRole{
		Name: "Billing", Permissions: []platformv1.PlatformPermission{platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_READ},
	}})
	require.NoError(t, err)

	group, err := f.srv.CreatePlatformGroup(admin, &platformv1.CreatePlatformGroupRequest{Group: &platformv1.PlatformStaffGroup{
		Name: "Billing support", RoleId: role.GetRole().GetId(),
	}})
	require.NoError(t, err)

	invited, err := f.srv.CreatePlatformInvitation(admin, &platformv1.CreatePlatformInvitationRequest{
		Email: "bill@x.test", GroupId: group.GetGroup().GetId(),
	})
	require.NoError(t, err)

	inv := invited.GetInvitation()
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_PENDING, inv.GetStatus())
	assert.Equal(t, "Bill", inv.GetFullName())
	assert.Equal(t, "Billing", inv.GetRoleName())
	assert.NotNil(t, inv.GetExpiresAt())

	listed, err := f.srv.ListPlatformInvitations(admin, &platformv1.ListPlatformInvitationsRequest{
		Status: platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_PENDING,
	})
	require.NoError(t, err)
	assert.Len(t, listed.GetData(), 1)

	bill := f.as(t, "bill")

	access0, err := f.srv.GetMyPlatformAccess(bill, &platformv1.GetMyPlatformAccessRequest{})
	require.NoError(t, err)
	assert.False(t, access0.GetAccess().GetPlatformAdmin())
	assert.Empty(t, access0.GetAccess().GetPermissions())
	assert.EqualValues(t, 1, access0.GetAccess().GetPendingInvitationCount())

	mine, err := f.srv.ListMyPlatformInvitations(bill, &platformv1.ListMyPlatformInvitationsRequest{})
	require.NoError(t, err)
	require.Len(t, mine.GetData(), 1)

	accepted, err := f.srv.AcceptPlatformInvitation(bill, &platformv1.AcceptPlatformInvitationRequest{InvitationId: inv.GetId()})
	require.NoError(t, err)
	assert.NotNil(t, accepted.GetInvitation().GetRespondedAt())

	access1, err := f.srv.GetMyPlatformAccess(f.as(t, "bill"), &platformv1.GetMyPlatformAccessRequest{})
	require.NoError(t, err)
	assert.Equal(t, []platformv1.PlatformPermission{platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_READ}, access1.GetAccess().GetPermissions())

	adminAccess, err := f.srv.GetMyPlatformAccess(admin, &platformv1.GetMyPlatformAccessRequest{})
	require.NoError(t, err)
	assert.True(t, adminAccess.GetAccess().GetPlatformAdmin())
	assert.Len(t, adminAccess.GetAccess().GetPermissions(), 12)

	staff, err := f.srv.ListPlatformStaff(admin, &platformv1.ListPlatformStaffRequest{})
	require.NoError(t, err)
	require.Len(t, staff.GetData(), 2)
	assert.True(t, staff.GetData()[0].GetPlatformAdmin())
	assert.Equal(t, "Billing support", staff.GetData()[1].GetGroups()[0].GetName())

	detail, err := f.srv.GetPlatformGroup(admin, &platformv1.GetPlatformGroupRequest{GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)
	require.Len(t, detail.GetMembers(), 1)
	assert.Equal(t, "bill@x.test", detail.GetMembers()[0].GetEmail())

	_, err = f.srv.RemovePlatformStaffMember(admin, &platformv1.RemovePlatformStaffMemberRequest{GroupId: group.GetGroup().GetId(), UserId: "bill"})
	require.NoError(t, err)

	_, err = f.srv.RemovePlatformStaffMember(admin, &platformv1.RemovePlatformStaffMemberRequest{GroupId: group.GetGroup().GetId(), UserId: "bill"})
	requireStatus(t, err, codes.NotFound)

	// Revoke and decline.
	second, err := f.srv.CreatePlatformInvitation(admin, &platformv1.CreatePlatformInvitationRequest{Email: "bill@x.test", GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)

	revoked, err := f.srv.RevokePlatformInvitation(admin, &platformv1.RevokePlatformInvitationRequest{InvitationId: second.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_REVOKED, revoked.GetInvitation().GetStatus())

	third, err := f.srv.CreatePlatformInvitation(admin, &platformv1.CreatePlatformInvitationRequest{Email: "bill@x.test", GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)

	declined, err := f.srv.DeclinePlatformInvitation(f.as(t, "bill"), &platformv1.DeclinePlatformInvitationRequest{InvitationId: third.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_DECLINED, declined.GetInvitation().GetStatus())

	_, err = f.srv.CreatePlatformInvitation(admin, &platformv1.CreatePlatformInvitationRequest{Email: "nobody@x.test", GroupId: group.GetGroup().GetId()})
	requireStatus(t, err, codes.NotFound)
}

func TestHandlersReportUseCaseErrors(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := f.as(t, "admin")
	f.repo.FailOn = "ListRoles"

	_, err := f.srv.ListPlatformRoles(ctx, &platformv1.ListPlatformRolesRequest{})
	requireStatus(t, err, codes.Internal)

	for name, call := range map[string]func() error{
		"GetPlatformGroup": func() error {
			_, err := f.srv.GetPlatformGroup(ctx, &platformv1.GetPlatformGroupRequest{GroupId: "x"})

			return err
		},
		"ListPlatformGroups": func() error {
			f.repo.FailOn = "ListGroups"
			_, err := f.srv.ListPlatformGroups(ctx, &platformv1.ListPlatformGroupsRequest{})

			return err
		},
		"ListPlatformStaff": func() error {
			f.repo.FailOn = "AdminIDs"
			_, err := f.srv.ListPlatformStaff(ctx, &platformv1.ListPlatformStaffRequest{})

			return err
		},
		"ListPlatformInvitations": func() error {
			f.repo.FailOn = "ListInvitations"
			_, err := f.srv.ListPlatformInvitations(ctx, &platformv1.ListPlatformInvitationsRequest{})

			return err
		},
		"ListMyPlatformInvitations": func() error {
			f.repo.FailOn = "ListInvitationsByUser"
			_, err := f.srv.ListMyPlatformInvitations(ctx, &platformv1.ListMyPlatformInvitationsRequest{})

			return err
		},
		"GetMyPlatformAccess": func() error {
			f.repo.FailOn = "PlatformAccess"
			_, err := f.srv.GetMyPlatformAccess(ctx, &platformv1.GetMyPlatformAccessRequest{})

			return err
		},
		"AcceptPlatformInvitation": func() error {
			_, err := f.srv.AcceptPlatformInvitation(ctx, &platformv1.AcceptPlatformInvitationRequest{InvitationId: "x"})

			return err
		},
		"DeclinePlatformInvitation": func() error {
			_, err := f.srv.DeclinePlatformInvitation(ctx, &platformv1.DeclinePlatformInvitationRequest{InvitationId: "x"})

			return err
		},
		"RevokePlatformInvitation": func() error {
			_, err := f.srv.RevokePlatformInvitation(ctx, &platformv1.RevokePlatformInvitationRequest{InvitationId: "x"})

			return err
		},
		"RemovePlatformStaffMember": func() error {
			_, err := f.srv.RemovePlatformStaffMember(ctx, &platformv1.RemovePlatformStaffMemberRequest{GroupId: "x"})

			return err
		},
		"DeletePlatformGroup": func() error {
			_, err := f.srv.DeletePlatformGroup(ctx, &platformv1.DeletePlatformGroupRequest{GroupId: "x"})

			return err
		},
		"UpdatePlatformGroup": func() error {
			_, err := f.srv.UpdatePlatformGroup(ctx, &platformv1.UpdatePlatformGroupRequest{GroupId: "x"})

			return err
		},
		"CreatePlatformGroup": func() error {
			_, err := f.srv.CreatePlatformGroup(ctx, &platformv1.CreatePlatformGroupRequest{Group: &platformv1.PlatformStaffGroup{}})

			return err
		},
		"CreatePlatformRole": func() error {
			_, err := f.srv.CreatePlatformRole(ctx, &platformv1.CreatePlatformRoleRequest{Role: &platformv1.PlatformRole{}})

			return err
		},
		"UpdatePlatformRole": func() error {
			_, err := f.srv.UpdatePlatformRole(ctx, &platformv1.UpdatePlatformRoleRequest{RoleId: "x"})

			return err
		},
		"DeletePlatformRole": func() error {
			_, err := f.srv.DeletePlatformRole(ctx, &platformv1.DeletePlatformRoleRequest{RoleId: "x"})

			return err
		},
		"CreatePlatformInvitation": func() error {
			_, err := f.srv.CreatePlatformInvitation(ctx, &platformv1.CreatePlatformInvitationRequest{GroupId: "x"})

			return err
		},
	} {
		f.repo.FailOn = ""

		require.Error(t, call(), name)
	}
}
