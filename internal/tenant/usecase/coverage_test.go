package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var (
	errTestRandom = errors.New("random")
	errTestDB     = errors.New("db error")
	errTestBoom   = errors.New("boom")
)

func TestToAppError(t *testing.T) {
	t.Parallel()

	uc := &usecase.TenantUseCase{}

	t.Run("already an AppError", func(t *testing.T) {
		err := apperror.Validation("field", apperror.IssueRequired, "required")
		result := uc.ToAppErrorForTest("op", err)
		assert.Equal(t, err, result)
	})

	t.Run("domain errors map correctly", func(t *testing.T) {
		tests := []struct {
			name   string
			err    error
			code   apperror.Code
			reason string
		}{
			{"garage not found", domain.ErrGarageNotFound, apperror.CodeNotFound, ""},
			{"staff not found", domain.ErrStaffNotFound, apperror.CodeNotFound, ""},
			{"role not found", domain.ErrRoleNotFound, apperror.CodeNotFound, ""},
			{"group not found", domain.ErrGroupNotFound, apperror.CodeNotFound, ""},
			{"invitation not found", domain.ErrInvitationNotFound, apperror.CodeNotFound, ""},
			{"user not found", domain.ErrUserNotFound, apperror.CodeNotFound, domain.ReasonUserNotFound},
			{"user inactive", domain.ErrUserInactive, apperror.CodeFailedPrecondition, domain.ReasonUserInactive},
			{"owner membership", domain.ErrOwnerMembership, apperror.CodeFailedPrecondition, domain.ReasonOwnerMembership},
			{"already member", domain.ErrAlreadyMember, apperror.CodeAlreadyExists, domain.ReasonAlreadyMember},
			{"already invited", domain.ErrAlreadyInvited, apperror.CodeAlreadyExists, domain.ReasonAlreadyInvited},
			{"invitation not pending", domain.ErrInvitationNotPending, apperror.CodeFailedPrecondition, domain.ReasonInvitationNotPending},
			{"invitation expired", domain.ErrInvitationExpired, apperror.CodeFailedPrecondition, domain.ReasonInvitationExpired},
			{"role name taken", domain.ErrRoleNameTaken, apperror.CodeAlreadyExists, domain.ReasonRoleNameTaken},
			{"role in use", domain.ErrRoleInUse, apperror.CodeFailedPrecondition, domain.ReasonRoleInUse},
			{"group name taken", domain.ErrGroupNameTaken, apperror.CodeAlreadyExists, domain.ReasonGroupNameTaken},
			{"invalid logo", domain.ErrInvalidLogo, apperror.CodeInvalidArgument, domain.ReasonInvalidLogo},
			{"unknown error becomes internal", errTestRandom, apperror.CodeInternal, ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := uc.ToAppErrorForTest("operation", tt.err)
				require.Error(t, result)
				appErr, ok := apperror.As(result)
				require.True(t, ok)

				assert.Equal(t, tt.code, appErr.Code)

				if tt.reason != "" {
					assert.Equal(t, tt.reason, appErr.ReasonOrDefault())
				}
			})
		}
	})
}

func TestNewTenantUseCase(t *testing.T) {
	t.Parallel()

	t.Run("sets default Now function", func(t *testing.T) {
		deps := usecase.Deps{}
		uc := usecase.NewTenantUseCase(deps)
		require.NotNil(t, uc)
		assert.NotNil(t, uc.Now)
	})

	t.Run("uses provided Now function", func(t *testing.T) {
		fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		deps := usecase.Deps{Now: func() time.Time { return fixedTime }}
		uc := usecase.NewTenantUseCase(deps)
		assert.Equal(t, fixedTime, uc.Now())
	})
}

func TestUserLocale(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	ctx := e.as(t, owner, "")

	t.Run("returns en for user with en locale", func(t *testing.T) {
		locale := e.UC.UserLocaleForTest(ctx, owner)
		assert.Equal(t, "en", locale)
	})

	t.Run("returns vi for user not found", func(t *testing.T) {
		locale := e.UC.UserLocaleForTest(ctx, "nonexistent")
		assert.Equal(t, "vi", locale)
	})

	t.Run("returns vi for user with error", func(t *testing.T) {
		e2 := newEnv(t, limits{Garages: 1, Staff: 5})
		e2.Dir.Err = errTestDB
		ctx2 := e2.as(t, owner, "")
		locale := e2.UC.UserLocaleForTest(ctx2, "nonexistent")
		assert.Equal(t, "vi", locale)
	})
}

func TestCallerID(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string for nil caller", func(t *testing.T) {
		result := usecase.CallerIDForTest(nil)
		assert.Empty(t, result)
	})

	t.Run("returns user ID for non-nil caller", func(t *testing.T) {
		caller := &access.Access{UserID: "user-123"}
		result := usecase.CallerIDForTest(caller)
		assert.Equal(t, "user-123", result)
	})
}

func TestApplyDocumentConfiguration(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	t.Run("validates payment voucher template", func(t *testing.T) {
		_, err := e.UC.UpdateGarage(ctx, id, usecase.GarageFields{
			PaymentVoucherNumberTemplate: new("PT-no-sequence"),
		})
		requireReason(t, err, apperror.ReasonValidation)
	})
}

func TestGroupCoverage(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	roles, err := e.UC.ListRoles(ctx, owner)
	require.NoError(t, err)

	t.Run("create group validation - missing name", func(t *testing.T) {
		_, err := e.UC.CreateGroup(ctx, id, usecase.GroupFields{RoleID: &roles[0].ID})
		requireReason(t, err, apperror.ReasonValidation)
	})

	t.Run("create group validation - missing role", func(t *testing.T) {
		name := "Test Group"
		_, err := e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: &name})
		requireReason(t, err, apperror.ReasonValidation)
	})

	t.Run("update group - not found", func(t *testing.T) {
		_, err := e.UC.UpdateGroup(ctx, id, "missing", usecase.GroupFields{})
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("update group - validation error on empty name", func(t *testing.T) {
		group, err := e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new("Valid"), RoleID: &roles[0].ID})
		require.NoError(t, err)

		name := ""
		_, err = e.UC.UpdateGroup(ctx, id, group.ID, usecase.GroupFields{Name: &name})
		requireReason(t, err, apperror.ReasonValidation)
	})

	t.Run("delete group - not found", func(t *testing.T) {
		err := e.UC.DeleteGroup(ctx, id, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("ensureDefaultGroups creates groups for default roles", func(t *testing.T) {
		e2 := newEnv(t, limits{Garages: 1, Staff: 5})
		id2 := e2.garage(t)
		ctx2 := e2.as(t, owner, id2)

		groups, err := e2.UC.ListGroups(ctx2, id2)
		require.NoError(t, err)
		assert.Len(t, groups, 5)
	})
}

func TestInvitationCoverage_Create(t *testing.T) {
	t.Parallel()

	t.Run("create invitation - invitee not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.CreateInvitation(ctx, id, "ghost", nil)
		requireReason(t, err, domain.ReasonUserNotFound)
	})

	t.Run("create invitation - invitee inactive", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.CreateInvitation(ctx, id, "off", nil)
		requireReason(t, err, domain.ReasonUserInactive)
	})

	t.Run("create invitation - already member", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		_, err = e.UC.CreateInvitation(ctx, id, staff, nil)
		requireReason(t, err, domain.ReasonAlreadyMember)
	})

	t.Run("create invitation - already invited", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		_, err = e.UC.CreateInvitation(ctx, id, staff, nil)
		requireReason(t, err, domain.ReasonAlreadyInvited)
	})
}

func TestInvitationCoverage_Revoke(t *testing.T) {
	t.Parallel()

	t.Run("revoke invitation - not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.RevokeInvitation(ctx, id, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("revoke invitation - wrong garage", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		_, err = e.UC.RevokeInvitation(ctx, "other-garage", inv.ID)
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("revoke invitation - already revoked", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		_, err = e.UC.RevokeInvitation(ctx, id, inv.ID)
		require.NoError(t, err)

		_, err = e.UC.RevokeInvitation(ctx, id, inv.ID)
		requireReason(t, err, domain.ReasonInvitationNotPending)
	})
}

func TestInvitationCoverage_Get(t *testing.T) {
	t.Parallel()

	t.Run("list my invitations - error", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		e.Store.Fail["invitation.list"] = errTestBoom
		_, err := e.UC.ListMyInvitations(ctx, owner, "")
		requireReason(t, err, apperror.ReasonInternal)
		delete(e.Store.Fail, "invitation.list")
	})

	t.Run("get my invitation - not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.GetMyInvitation(ctx, owner, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("get my invitation - wrong user", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		_, err = e.UC.GetMyInvitation(ctx, owner, inv.ID)
		requireReason(t, err, apperror.ReasonNotFound)
	})
}

func TestInvitationCoverage_AcceptDecline(t *testing.T) {
	t.Parallel()

	t.Run("accept invitation - not pending", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		revoked, err := e.UC.RevokeInvitation(ctx, id, inv.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.InvitationRevoked, revoked.Status)

		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		requireReason(t, err, domain.ReasonInvitationNotPending)
	})

	t.Run("accept invitation - expired", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		e.Now = e.Now.Add(domain.InvitationTTL + time.Hour)

		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		requireReason(t, err, domain.ReasonInvitationExpired)
	})

	t.Run("decline invitation - not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		_ = e.as(t, owner, id)

		_, err := e.UC.DeclineInvitation(e.as(t, staff, ""), staff, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("decline invitation - wrong user", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)

		_, err = e.UC.DeclineInvitation(e.as(t, owner, ""), owner, inv.ID)
		requireReason(t, err, apperror.ReasonNotFound)
	})
}

func TestRoleCoverage(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	ctx := e.as(t, owner, "")

	t.Run("update role - not found", func(t *testing.T) {
		_, err := e.UC.UpdateRole(ctx, owner, "missing", usecase.RoleFields{})
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("update role - validation error", func(t *testing.T) {
		perms := []access.Permission{access.ReportRead}
		role, err := e.UC.CreateRole(ctx, owner, usecase.RoleFields{Name: new("Test"), Permissions: &perms})
		require.NoError(t, err)

		name := ""
		_, err = e.UC.UpdateRole(ctx, owner, role.ID, usecase.RoleFields{Name: &name})
		requireReason(t, err, apperror.ReasonValidation)
	})

	t.Run("delete role - not found", func(t *testing.T) {
		err := e.UC.DeleteRole(ctx, owner, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("delete role - in use", func(t *testing.T) {
		perms := []access.Permission{access.ReportRead}
		role, err := e.UC.CreateRole(ctx, owner, usecase.RoleFields{Name: new("Test Role"), Permissions: &perms})
		require.NoError(t, err)

		id := e.garage(t)
		ctx2 := e.as(t, owner, id)
		_, err = e.UC.CreateGroup(ctx2, id, usecase.GroupFields{Name: new("Group"), RoleID: &role.ID})
		require.NoError(t, err)

		err = e.UC.DeleteRole(ctx, owner, role.ID)
		requireReason(t, err, domain.ReasonRoleInUse)
	})
}

func TestStaffCoverage_Update(t *testing.T) {
	t.Parallel()

	t.Run("update staff - not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		members, err := e.UC.ListStaff(ctx, id)
		require.NoError(t, err)

		_ = members[1].ID

		_, err = e.UC.UpdateStaff(ctx, id, "missing", usecase.StaffFields{})
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("update staff - invalid status", func(t *testing.T) {
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

		status := domain.StaffStatus("invalid")
		_, err = e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{Status: &status})
		requireReason(t, err, apperror.ReasonValidation)
	})

	t.Run("update staff - owner cannot be deactivated", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		members, err := e.UC.ListStaff(ctx, id)
		require.NoError(t, err)

		ownerID := members[0].ID

		_, err = e.UC.UpdateStaff(ctx, id, ownerID, usecase.StaffFields{Status: new(domain.StaffStatusInactive)})
		requireReason(t, err, domain.ReasonOwnerMembership)
	})
}

func TestStaffCoverage_Delete(t *testing.T) {
	t.Parallel()

	t.Run("delete staff - not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		err = e.UC.DeleteStaff(ctx, id, "missing")
		requireReason(t, err, apperror.ReasonNotFound)
	})

	t.Run("delete staff - already deleted", func(t *testing.T) {
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

		require.NoError(t, e.UC.DeleteStaff(ctx, id, memberID))
		err = e.UC.DeleteStaff(ctx, id, memberID)
		requireReason(t, err, apperror.ReasonNotFound)

		_ = memberID
	})
}

func TestStaffCoverage_Status(t *testing.T) {
	t.Parallel()

	t.Run("change staff status - reactivation checks limit", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 1})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		members, err := e.UC.ListStaff(ctx, id)
		require.NoError(t, err)

		memberID := members[1].ID

		_, err = e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{Status: new(domain.StaffStatusInactive)})
		require.NoError(t, err)

		_, err = e.UC.UpdateStaff(ctx, id, memberID, usecase.StaffFields{Status: new(domain.StaffStatusActive)})
		require.NoError(t, err)
	})

	t.Run("checkGarageOwnerUsage - error", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		inv, err := e.UC.CreateInvitation(ctx, id, staff, nil)
		require.NoError(t, err)
		_, err = e.UC.AcceptInvitation(e.as(t, staff, ""), staff, inv.ID)
		require.NoError(t, err)

		e.Store.Fail["garage.find"] = errTestBoom
		err = e.UC.CheckGarageOwnerUsageForTest(ctx, id, usecase.LimitStaff)
		requireReason(t, err, apperror.ReasonInternal)
		delete(e.Store.Fail, "garage.find")
	})
}

func TestLookupCandidateCoverage(t *testing.T) {
	t.Parallel()

	t.Run("user not found", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.LookupCandidate(ctx, id, "nonexistent@example.com")
		requireReason(t, err, domain.ReasonUserNotFound)
	})

	t.Run("user inactive", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)

		_, err := e.UC.LookupCandidate(ctx, id, "off@example.com")
		requireReason(t, err, domain.ReasonUserInactive)
	})

	t.Run("findUserByContact error", func(t *testing.T) {
		e := newEnv(t, limits{Garages: 1, Staff: 5})
		id := e.garage(t)
		ctx := e.as(t, owner, id)
		_ = ctx

		e.Dir.Err = errTestDB
		_, err := e.UC.LookupCandidate(ctx, id, "unknown@example.com")
		requireReason(t, err, domain.ReasonUserNotFound)
	})
}
