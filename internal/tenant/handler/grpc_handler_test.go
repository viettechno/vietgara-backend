package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/genproto/googleapis/type/dayofweek"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/tenant/handler"
	"github.com/viettechno/vietgara-backend/internal/tenant/tenanttest"
)

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

type env struct {
	*tenanttest.Env
	srv *handler.Server
}

func newEnv() *env {
	e := tenanttest.NewEnv(tenanttest.Limits{Garages: 2, Staff: 5})

	return &env{Env: e, srv: handler.NewServer(e.UC)}
}

func (e *env) as(t *testing.T, userID, garageID string) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = userID
	ctx := middleware.WithClaims(t.Context(), claims)

	if garageID != "" {
		user, err := e.Store.GarageAccess(ctx, garageID, userID)
		require.NoError(t, err)

		ctx = access.WithAccess(ctx, user)
	}

	return ctx
}

func mask(paths ...string) *fieldmaskpb.FieldMask { return &fieldmaskpb.FieldMask{Paths: paths} }

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 27)
	assert.True(t, p[tenantv1.GarageService_GetGarage_FullMethodName].SkipLicense)
	assert.True(t, p[tenantv1.StaffGroupService_CreateStaffGroup_FullMethodName].Garage.OwnerOnly)
	assert.Equal(t, access.StaffWrite, p[tenantv1.InvitationService_CreateInvitation_FullMethodName].Garage.Permission)
}

func TestSubscribe(t *testing.T) {
	t.Parallel()

	d := outbox.NewDispatcher()
	handler.Subscribe(d, outbox.NewProcessor(nil, nil), newEnv().UC)
	assert.Len(t, d.Handlers("user.email_verified"), 1)
}

func TestGarageRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv()
	ownerCtx := e.as(t, tenanttest.Owner, "")
	tax := "0101"

	created, err := e.srv.CreateGarage(ownerCtx, &tenantv1.CreateGarageRequest{Garage: &tenantv1.Garage{
		Name: "Gara", TaxCode: &tax, Timezone: "Asia/Bangkok", Currency: "usd",
		OperatingHours: []*tenantv1.DailyHours{{Day: dayofweek.DayOfWeek_MONDAY, Open: "08:00", Close: "18:00"}},
		Numbering:      &tenantv1.NumberingTemplates{Quote: "Q-{SEQ:3}"},
	}})
	require.NoError(t, err)

	g := created.GetGarage()
	assert.Equal(t, "USD", g.GetCurrency())
	assert.Equal(t, "Q-{SEQ:3}", g.GetNumbering().GetQuote())
	assert.Equal(t, "LSC-{YYYY}{MM}-{SEQ:4}", g.GetNumbering().GetRepairOrder())
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE, g.GetStatus())
	assert.True(t, g.GetAccess().GetOwner())
	assert.Len(t, g.GetAccess().GetPermissions(), 20)
	assert.Len(t, g.GetOperatingHours(), 1)

	_, err = e.srv.CreateGarage(ownerCtx, &tenantv1.CreateGarageRequest{Garage: &tenantv1.Garage{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	list, err := e.srv.ListGarages(ownerCtx, &tenantv1.ListGaragesRequest{})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	gctx := e.as(t, tenanttest.Owner, g.GetId())

	got, err := e.srv.GetGarage(gctx, &tenantv1.GetGarageRequest{GarageId: g.GetId()})
	require.NoError(t, err)
	assert.Equal(t, "Gara", got.GetGarage().GetName())

	_, err = e.srv.GetGarage(gctx, &tenantv1.GetGarageRequest{GarageId: "missing"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	updated, err := e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId: g.GetId(),
		Garage: &tenantv1.Garage{
			Name: "Renamed", Status: tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE,
			Numbering: &tenantv1.NumberingTemplates{Quote: "A-{SEQ:2}", RepairOrder: "B-{SEQ:2}", Settlement: "C-{SEQ:2}"},
		},
		UpdateMask: mask("name", "status", "numbering"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.GetGarage().GetName())
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE, updated.GetGarage().GetStatus())
	assert.Equal(t, "B-{SEQ:2}", updated.GetGarage().GetNumbering().GetRepairOrder())

	withVoucher, err := e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId: g.GetId(),
		Garage: &tenantv1.Garage{Numbering: &tenantv1.NumberingTemplates{
			Quote: "A-{SEQ:2}", RepairOrder: "B-{SEQ:2}", Settlement: "C-{SEQ:2}", PaymentVoucher: "PT-{SEQ:3}",
		}},
		UpdateMask: mask("numbering"),
	})
	require.NoError(t, err)
	assert.Equal(t, "PT-{SEQ:3}", withVoucher.GetGarage().GetNumbering().GetPaymentVoucher())

	for _, m := range []*fieldmaskpb.FieldMask{mask("owner_user_id"), mask("status"), nil} {
		_, err = e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{GarageId: g.GetId(), Garage: &tenantv1.Garage{}, UpdateMask: m})
		assert.Equal(t, "VALIDATION_ERROR", reason(err))
	}

	phone, email, addr := "0901", "g@b.vn", "addr"
	_, err = e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId:   g.GetId(),
		Garage:     &tenantv1.Garage{Phone: &phone, Email: &email, Address: &addr, TaxCode: &tax, Timezone: "Asia/Ho_Chi_Minh", Currency: "VND", Numbering: &tenantv1.NumberingTemplates{RepairOrder: "R-{SEQ:4}"}},
		UpdateMask: mask("phone", "email", "address", "tax_code", "timezone", "currency", "operating_hours", "numbering.repair_order"),
	})
	require.NoError(t, err)

	logo, err := e.srv.UploadGarageLogo(gctx, &tenantv1.UploadGarageLogoRequest{GarageId: g.GetId(), Content: []byte("\x89PNG\r\n\x1a\n...."), ContentType: "image/png"})
	require.NoError(t, err)
	assert.NotEmpty(t, logo.GetGarage().GetLogoUrl())

	_, err = e.srv.UploadGarageLogo(gctx, &tenantv1.UploadGarageLogoRequest{GarageId: g.GetId(), ContentType: "image/png"})
	assert.Equal(t, "INVALID_LOGO", reason(err))

	cleared, err := e.srv.DeleteGarageLogo(gctx, &tenantv1.DeleteGarageLogoRequest{GarageId: g.GetId()})
	require.NoError(t, err)
	assert.Nil(t, cleared.GetGarage().LogoUrl)

	_, err = e.srv.DeleteGarageLogo(gctx, &tenantv1.DeleteGarageLogoRequest{GarageId: "missing"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	e.Store.Fail["garage.list"] = assert.AnError
	_, err = e.srv.ListGarages(ownerCtx, &tenantv1.ListGaragesRequest{})
	assert.Empty(t, reason(err))
}

func TestStaffAndInvitationRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv()
	created, err := e.srv.CreateGarage(e.as(t, tenanttest.Owner, ""), &tenantv1.CreateGarageRequest{Garage: &tenantv1.Garage{Name: "G"}})
	require.NoError(t, err)

	gid := created.GetGarage().GetId()
	ctx := e.as(t, tenanttest.Owner, gid)
	staffCtx := e.as(t, tenanttest.Staff, "")

	groups, err := e.srv.ListStaffGroups(ctx, &tenantv1.ListStaffGroupsRequest{GarageId: gid})
	require.NoError(t, err)
	require.Len(t, groups.GetData(), 5)
	groupID := groups.GetData()[0].GetId()

	cand, err := e.srv.LookupStaffCandidate(ctx, &tenantv1.LookupStaffCandidateRequest{GarageId: gid, Query: "lan@example.com"})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.CandidateMembership_CANDIDATE_MEMBERSHIP_NONE, cand.GetCandidate().GetMembership())

	_, err = e.srv.LookupStaffCandidate(ctx, &tenantv1.LookupStaffCandidateRequest{GarageId: gid, Query: "ghost@x.vn"})
	assert.Equal(t, "USER_NOT_FOUND", reason(err))

	inv, err := e.srv.CreateInvitation(ctx, &tenantv1.CreateInvitationRequest{GarageId: gid, UserId: tenanttest.Staff, GroupIds: []string{groupID}})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_PENDING, inv.GetInvitation().GetStatus())

	_, err = e.srv.CreateInvitation(ctx, &tenantv1.CreateInvitationRequest{GarageId: gid, UserId: tenanttest.Staff})
	assert.Equal(t, "ALREADY_INVITED", reason(err))

	list, err := e.srv.ListInvitations(ctx, &tenantv1.ListInvitationsRequest{GarageId: gid, Status: tenantv1.InvitationStatus_INVITATION_STATUS_PENDING})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	mine, err := e.srv.ListMyInvitations(staffCtx, &tenantv1.ListMyInvitationsRequest{})
	require.NoError(t, err)
	assert.Len(t, mine.GetData(), 1)

	one, err := e.srv.GetMyInvitation(staffCtx, &tenantv1.GetMyInvitationRequest{InvitationId: inv.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.Equal(t, "G", one.GetInvitation().GetGarageName())

	_, err = e.srv.GetMyInvitation(staffCtx, &tenantv1.GetMyInvitationRequest{InvitationId: "x"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	accepted, err := e.srv.AcceptInvitation(staffCtx, &tenantv1.AcceptInvitationRequest{InvitationId: inv.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.NotNil(t, accepted.GetInvitation().GetRespondedAt())

	_, err = e.srv.AcceptInvitation(staffCtx, &tenantv1.AcceptInvitationRequest{InvitationId: inv.GetInvitation().GetId()})
	assert.Equal(t, "INVITATION_NOT_PENDING", reason(err))
	_, err = e.srv.DeclineInvitation(staffCtx, &tenantv1.DeclineInvitationRequest{InvitationId: inv.GetInvitation().GetId()})
	assert.Equal(t, "INVITATION_NOT_PENDING", reason(err))
	_, err = e.srv.RevokeInvitation(ctx, &tenantv1.RevokeInvitationRequest{GarageId: gid, InvitationId: inv.GetInvitation().GetId()})
	assert.Equal(t, "INVITATION_NOT_PENDING", reason(err))

	members, err := e.srv.ListStaff(ctx, &tenantv1.ListStaffRequest{GarageId: gid})
	require.NoError(t, err)
	require.Len(t, members.GetData(), 2)
	ownerRow, member := members.GetData()[0], members.GetData()[1]
	assert.True(t, ownerRow.GetOwner())
	assert.Len(t, ownerRow.GetPermissions(), 20)
	assert.Equal(t, []string{groupID}, member.GetGroupIds())

	updated, err := e.srv.UpdateStaff(ctx, &tenantv1.UpdateStaffRequest{
		GarageId: gid, StaffId: member.GetId(),
		Staff: &tenantv1.Staff{Status: tenantv1.StaffStatus_STAFF_STATUS_INACTIVE, GroupIds: []string{}},
	})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_INACTIVE, updated.GetStaff().GetStatus())
	assert.Empty(t, updated.GetStaff().GetGroupIds())

	_, err = e.srv.UpdateStaff(ctx, &tenantv1.UpdateStaffRequest{GarageId: gid, StaffId: member.GetId(), Staff: &tenantv1.Staff{}, UpdateMask: mask("user_id")})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = e.srv.UpdateStaff(ctx, &tenantv1.UpdateStaffRequest{GarageId: gid, StaffId: member.GetId(), Staff: &tenantv1.Staff{}, UpdateMask: mask("status")})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	_, err = e.srv.DeleteStaff(ctx, &tenantv1.DeleteStaffRequest{GarageId: gid, StaffId: member.GetId()})
	require.NoError(t, err)
	_, err = e.srv.DeleteStaff(ctx, &tenantv1.DeleteStaffRequest{GarageId: gid, StaffId: ownerRow.GetId()})
	assert.Equal(t, "OWNER_MEMBERSHIP", reason(err))

	second, err := e.srv.CreateInvitation(ctx, &tenantv1.CreateInvitationRequest{GarageId: gid, UserId: tenanttest.Staff})
	require.NoError(t, err)
	declined, err := e.srv.DeclineInvitation(staffCtx, &tenantv1.DeclineInvitationRequest{InvitationId: second.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_DECLINED, declined.GetInvitation().GetStatus())

	third, err := e.srv.CreateInvitation(ctx, &tenantv1.CreateInvitationRequest{GarageId: gid, UserId: tenanttest.Staff})
	require.NoError(t, err)
	revoked, err := e.srv.RevokeInvitation(ctx, &tenantv1.RevokeInvitationRequest{GarageId: gid, InvitationId: third.GetInvitation().GetId()})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.InvitationStatus_INVITATION_STATUS_REVOKED, revoked.GetInvitation().GetStatus())

	e.Dir.Err = assert.AnError

	for _, call := range []func() error{
		func() error { _, err := e.srv.ListStaff(ctx, &tenantv1.ListStaffRequest{GarageId: gid}); return err },
		func() error {
			_, err := e.srv.ListInvitations(ctx, &tenantv1.ListInvitationsRequest{GarageId: gid})
			return err
		},
		func() error {
			_, err := e.srv.ListMyInvitations(staffCtx, &tenantv1.ListMyInvitationsRequest{})
			return err
		},
	} {
		require.Error(t, call())
	}
}

func TestRoleAndGroupRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv()
	ownerCtx := e.as(t, tenanttest.Owner, "")

	perms, err := e.srv.ListPermissions(ownerCtx, &tenantv1.ListPermissionsRequest{})
	require.NoError(t, err)
	assert.Len(t, perms.GetData(), 20)
	assert.Equal(t, "garage", perms.GetData()[0].GetModule())
	assert.Equal(t, "write", perms.GetData()[0].GetAction())
	assert.Equal(t, "read", perms.GetData()[1].GetAction())

	created, err := e.srv.CreateRole(ownerCtx, &tenantv1.CreateRoleRequest{Role: &tenantv1.Role{
		Name: "Lễ tân", Permissions: []tenantv1.Permission{tenantv1.Permission_PERMISSION_CUSTOMER_READ},
	}})
	require.NoError(t, err)

	roleID := created.GetRole().GetId()
	assert.False(t, created.GetRole().GetIsDefault())

	_, err = e.srv.CreateRole(ownerCtx, &tenantv1.CreateRoleRequest{Role: &tenantv1.Role{Name: "X", Permissions: []tenantv1.Permission{tenantv1.Permission_PERMISSION_UNSPECIFIED}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = e.srv.CreateRole(ownerCtx, &tenantv1.CreateRoleRequest{Role: &tenantv1.Role{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	got, err := e.srv.GetRole(ownerCtx, &tenantv1.GetRoleRequest{RoleId: roleID})
	require.NoError(t, err)
	assert.Equal(t, "Lễ tân", got.GetRole().GetName())
	_, err = e.srv.GetRole(ownerCtx, &tenantv1.GetRoleRequest{RoleId: "x"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	updated, err := e.srv.UpdateRole(ownerCtx, &tenantv1.UpdateRoleRequest{RoleId: roleID, Role: &tenantv1.Role{Description: "desk"}, UpdateMask: mask("description")})
	require.NoError(t, err)
	assert.Equal(t, "desk", updated.GetRole().GetDescription())

	_, err = e.srv.UpdateRole(ownerCtx, &tenantv1.UpdateRoleRequest{RoleId: roleID, Role: &tenantv1.Role{}, UpdateMask: mask("is_default")})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = e.srv.UpdateRole(ownerCtx, &tenantv1.UpdateRoleRequest{RoleId: roleID, Role: &tenantv1.Role{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err), "empty name with no mask")

	garage, err := e.srv.CreateGarage(ownerCtx, &tenantv1.CreateGarageRequest{Garage: &tenantv1.Garage{Name: "G"}})
	require.NoError(t, err)

	gid := garage.GetGarage().GetId()
	gctx := e.as(t, tenanttest.Owner, gid)

	roles, err := e.srv.ListRoles(ownerCtx, &tenantv1.ListRolesRequest{})
	require.NoError(t, err)
	assert.Len(t, roles.GetData(), 6)

	// Default roles expose their template permissions for "Reset to
	// default"; the owner's own roles do not.
	for _, r := range roles.GetData() {
		if !r.GetIsDefault() {
			assert.Empty(t, r.GetDefaultPermissions(), r.GetName())
			continue
		}

		assert.NotEmpty(t, r.GetDefaultPermissions(), r.GetName())
	}

	group, err := e.srv.CreateStaffGroup(gctx, &tenantv1.CreateStaffGroupRequest{GarageId: gid, Group: &tenantv1.StaffGroup{Name: "Desk", RoleId: roleID}})
	require.NoError(t, err)
	assert.Equal(t, "Lễ tân", group.GetGroup().GetRoleName())

	_, err = e.srv.CreateStaffGroup(gctx, &tenantv1.CreateStaffGroupRequest{GarageId: gid, Group: &tenantv1.StaffGroup{Name: "Desk", RoleId: roleID}})
	assert.Equal(t, "GROUP_NAME_TAKEN", reason(err))

	renamed, err := e.srv.UpdateStaffGroup(gctx, &tenantv1.UpdateStaffGroupRequest{GarageId: gid, GroupId: group.GetGroup().GetId(), Group: &tenantv1.StaffGroup{Name: "Front"}, UpdateMask: mask("name")})
	require.NoError(t, err)
	assert.Equal(t, "Front", renamed.GetGroup().GetName())
	_, err = e.srv.UpdateStaffGroup(gctx, &tenantv1.UpdateStaffGroupRequest{GarageId: gid, GroupId: group.GetGroup().GetId(), Group: &tenantv1.StaffGroup{}, UpdateMask: mask("garage_id")})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = e.srv.UpdateStaffGroup(gctx, &tenantv1.UpdateStaffGroupRequest{GarageId: gid, GroupId: group.GetGroup().GetId(), Group: &tenantv1.StaffGroup{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	_, err = e.srv.DeleteRole(ownerCtx, &tenantv1.DeleteRoleRequest{RoleId: roleID})
	assert.Equal(t, "ROLE_IN_USE", reason(err))

	_, err = e.srv.DeleteStaffGroup(gctx, &tenantv1.DeleteStaffGroupRequest{GarageId: gid, GroupId: group.GetGroup().GetId()})
	require.NoError(t, err)
	_, err = e.srv.DeleteStaffGroup(gctx, &tenantv1.DeleteStaffGroupRequest{GarageId: gid, GroupId: "x"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	_, err = e.srv.DeleteRole(ownerCtx, &tenantv1.DeleteRoleRequest{RoleId: roleID})
	require.NoError(t, err)

	e.Store.Fail["role.list"] = assert.AnError
	_, err = e.srv.ListRoles(ownerCtx, &tenantv1.ListRolesRequest{})
	require.Error(t, err)

	e.Store.Fail["group.list"] = assert.AnError
	_, err = e.srv.ListStaffGroups(gctx, &tenantv1.ListStaffGroupsRequest{GarageId: gid})
	require.Error(t, err)
}

// TestLegalProfileRPCs covers FR-TEN-06 through the API: create with the
// legal identity, change it by mask, and the voucher numbering template.
func TestLegalProfileRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv()
	ownerCtx := e.as(t, tenanttest.Owner, "")

	created, err := e.srv.CreateGarage(ownerCtx, &tenantv1.CreateGarageRequest{Garage: &tenantv1.Garage{
		Name: "Gara", LegalName: new("Cong ty TNHH Gara"), LegalAddress: new("12 Nguyen Hue"), Representative: new("An"),
		Bank:      &commonv1.BankDetails{BankName: new("VCB"), AccountNumber: new("001")},
		Numbering: &tenantv1.NumberingTemplates{PaymentVoucher: "PT-{SEQ:3}"},
	}})
	require.NoError(t, err)

	g := created.GetGarage()
	assert.Equal(t, "Cong ty TNHH Gara", g.GetLegalName())
	assert.Equal(t, "An", g.GetRepresentative())
	assert.Equal(t, "VCB", g.GetBank().GetBankName())
	assert.Equal(t, "PT-{SEQ:3}", g.GetNumbering().GetPaymentVoucher())

	gctx := e.as(t, tenanttest.Owner, g.GetId())

	updated, err := e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId: g.GetId(),
		Garage: &tenantv1.Garage{
			LegalName: new("Gara Moi"), Bank: &commonv1.BankDetails{AccountHolder: new("An")},
			Numbering: &tenantv1.NumberingTemplates{PaymentVoucher: "PV-{YY}{SEQ:4}"},
		},
		UpdateMask: mask("legal_name", "bank", "numbering.payment_voucher"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Gara Moi", updated.GetGarage().GetLegalName())
	assert.Equal(t, "An", updated.GetGarage().GetRepresentative(), "unnamed fields are kept")
	assert.Empty(t, updated.GetGarage().GetBank().GetBankName(), "the bank block is replaced")
	assert.Equal(t, "An", updated.GetGarage().GetBank().GetAccountHolder())
	assert.Equal(t, "PV-{YY}{SEQ:4}", updated.GetGarage().GetNumbering().GetPaymentVoucher())

	cleared, err := e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId: g.GetId(), Garage: &tenantv1.Garage{}, UpdateMask: mask("legal_address", "bank"),
	})
	require.NoError(t, err)
	assert.Nil(t, cleared.GetGarage().LegalAddress)
	assert.Empty(t, cleared.GetGarage().GetBank().GetAccountHolder(), "an absent bank block clears the account")

	_, err = e.srv.UpdateGarage(gctx, &tenantv1.UpdateGarageRequest{
		GarageId: g.GetId(), Garage: &tenantv1.Garage{Numbering: &tenantv1.NumberingTemplates{PaymentVoucher: "no sequence"}},
		UpdateMask: mask("numbering.payment_voucher"),
	})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
}
