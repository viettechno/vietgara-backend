package handler

import (
	"strings"

	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

const (
	pathName        = "name"
	pathDescription = "description"
	pathPermissions = "permissions"
	pathRoleID      = "role_id"
)

// permissionToProto maps "plan.read" to PLATFORM_PERMISSION_PLAN_READ.
func permissionToProto(p access.PlatformPermission) platformv1.PlatformPermission {
	name := "PLATFORM_PERMISSION_" + strings.ToUpper(strings.ReplaceAll(string(p), ".", "_"))

	return platformv1.PlatformPermission(platformv1.PlatformPermission_value[name])
}

func permissionsToProto(perms []access.PlatformPermission) []platformv1.PlatformPermission {
	out := make([]platformv1.PlatformPermission, 0, len(perms))
	for _, p := range perms {
		out = append(out, permissionToProto(p))
	}

	return out
}

// permissionsFromProto maps API enum values back to catalogue permissions;
// ok is false for an unspecified or unknown value.
func permissionsFromProto(perms []platformv1.PlatformPermission) ([]access.PlatformPermission, bool) {
	out := make([]access.PlatformPermission, 0, len(perms))

	for _, in := range perms {
		found := false

		for _, e := range access.PlatformCatalog {
			if permissionToProto(e.Permission) == in {
				out = append(out, e.Permission)
				found = true

				break
			}
		}

		if !found {
			return nil, false
		}
	}

	return out, true
}

func roleToProto(r *domain.Role) *platformv1.PlatformRole {
	return &platformv1.PlatformRole{
		Id: r.ID, Name: r.Name, Description: r.Description,
		Permissions: permissionsToProto(r.Permissions),
		GroupCount:  int32(r.GroupCount), //nolint:gosec // a handful of groups
		CreatedAt:   converter.TimeToProto(r.CreatedAt),
		UpdatedAt:   converter.TimeToProto(r.UpdatedAt),
	}
}

func groupToProto(g *domain.Group) *platformv1.PlatformStaffGroup {
	return &platformv1.PlatformStaffGroup{
		Id: g.ID, Name: g.Name, Description: g.Description, RoleId: g.RoleID, RoleName: g.RoleName,
		MemberCount: int32(g.MemberCount), //nolint:gosec // a handful of members
		CreatedAt:   converter.TimeToProto(g.CreatedAt),
		UpdatedAt:   converter.TimeToProto(g.UpdatedAt),
	}
}

func membersToProto(members []*domain.Member) []*platformv1.PlatformStaffMember {
	out := make([]*platformv1.PlatformStaffMember, 0, len(members))

	for _, m := range members {
		groups := make([]*platformv1.GroupRef, 0, len(m.Groups))
		for _, g := range m.Groups {
			groups = append(groups, &platformv1.GroupRef{Id: g.ID, Name: g.Name})
		}

		out = append(out, &platformv1.PlatformStaffMember{
			UserId: m.UserID, FullName: m.FullName, Email: m.Email, PlatformAdmin: m.Admin, Groups: groups,
		})
	}

	return out
}

var invitationStatuses = map[domain.InvitationStatus]platformv1.PlatformInvitationStatus{
	domain.InvitationPending:  platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_PENDING,
	domain.InvitationAccepted: platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_ACCEPTED,
	domain.InvitationDeclined: platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_DECLINED,
	domain.InvitationRevoked:  platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_REVOKED,
	domain.InvitationExpired:  platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_EXPIRED,
}

// invitationStatusFromProto maps UNSPECIFIED to "" (every status).
func invitationStatusFromProto(s platformv1.PlatformInvitationStatus) domain.InvitationStatus {
	for status, p := range invitationStatuses {
		if p == s {
			return status
		}
	}

	return ""
}

func invitationToProto(inv *domain.Invitation) *platformv1.PlatformInvitation {
	out := &platformv1.PlatformInvitation{
		Id: inv.ID, GroupId: inv.GroupID, GroupName: inv.GroupName, RoleName: inv.RoleName,
		UserId: inv.UserID, FullName: inv.FullName, Email: inv.Email,
		Status:        invitationStatuses[inv.Status],
		InvitedByName: inv.InvitedByName,
		ExpiresAt:     converter.TimeToProto(inv.ExpiresAt),
		CreatedAt:     converter.TimeToProto(inv.CreatedAt),
	}

	if inv.RespondedAt != nil {
		out.RespondedAt = converter.TimeToProto(*inv.RespondedAt)
	}

	return out
}

func invitationsToProto(invitations []*domain.Invitation) []*platformv1.PlatformInvitation {
	out := make([]*platformv1.PlatformInvitation, 0, len(invitations))
	for _, inv := range invitations {
		out = append(out, invitationToProto(inv))
	}

	return out
}

func roleFields(in *platformv1.PlatformRole, paths []string) (usecase.RoleFields, error) {
	var f usecase.RoleFields

	for _, path := range paths {
		switch path {
		case pathName:
			f.Name = new(in.GetName())
		case pathDescription:
			f.Description = new(in.GetDescription())
		case pathPermissions:
			perms, ok := permissionsFromProto(in.GetPermissions())
			if !ok {
				return f, apperror.Validation("permissions", apperror.IssueInvalid, "unknown permission")
			}

			f.Permissions = &perms
		default:
			return f, apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
		}
	}

	return f, nil
}

func groupFields(in *platformv1.PlatformStaffGroup, paths []string) usecase.GroupFields {
	var f usecase.GroupFields

	for _, path := range paths {
		switch path {
		case pathName:
			f.Name = new(in.GetName())
		case pathDescription:
			f.Description = new(in.GetDescription())
		case pathRoleID:
			f.RoleID = new(in.GetRoleId())
		}
	}

	return f
}

// maskPaths returns the update mask, or every settable field when it is
// empty. The JSON field names of a mask (roleId) are accepted too.
func maskPaths(paths []string, settable ...string) []string {
	if len(paths) == 0 {
		return settable
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "roleId" {
			p = pathRoleID
		}

		out = append(out, p)
	}

	return out
}
