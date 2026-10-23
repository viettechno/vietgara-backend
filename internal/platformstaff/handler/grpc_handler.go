// Package handler exposes platform staff (Release 8.1) as gRPC services
// (REST through grpc-gateway): roles, groups, staff and invitations, and the
// caller's own platform access.
package handler

import (
	"context"

	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements the platform staff gRPC services.
type Server struct {
	platformv1.UnimplementedPlatformStaffServiceServer
	platformv1.UnimplementedPlatformAccessServiceServer

	uc *usecase.UseCase
}

// NewServer returns a Server backed by uc.
func NewServer(uc *usecase.UseCase) *Server { return &Server{uc: uc} }

// Policies declares what every platform staff RPC requires. The user-level
// RPCs need no platform permission: an invitee has none yet.
func Policies() middleware.Policies {
	read := middleware.PlatformMethod(access.PlatformStaffRead)
	write := middleware.PlatformMethod(access.PlatformStaffWrite)

	return middleware.Policies{
		platformv1.PlatformStaffService_ListPlatformPermissions_FullMethodName:   read,
		platformv1.PlatformStaffService_ListPlatformRoles_FullMethodName:         read,
		platformv1.PlatformStaffService_GetPlatformRole_FullMethodName:           read,
		platformv1.PlatformStaffService_CreatePlatformRole_FullMethodName:        write,
		platformv1.PlatformStaffService_UpdatePlatformRole_FullMethodName:        write,
		platformv1.PlatformStaffService_DeletePlatformRole_FullMethodName:        write,
		platformv1.PlatformStaffService_ListPlatformGroups_FullMethodName:        read,
		platformv1.PlatformStaffService_GetPlatformGroup_FullMethodName:          read,
		platformv1.PlatformStaffService_CreatePlatformGroup_FullMethodName:       write,
		platformv1.PlatformStaffService_UpdatePlatformGroup_FullMethodName:       write,
		platformv1.PlatformStaffService_DeletePlatformGroup_FullMethodName:       write,
		platformv1.PlatformStaffService_ListPlatformStaff_FullMethodName:         read,
		platformv1.PlatformStaffService_RemovePlatformStaffMember_FullMethodName: write,
		platformv1.PlatformStaffService_CreatePlatformInvitation_FullMethodName:  write,
		platformv1.PlatformStaffService_ListPlatformInvitations_FullMethodName:   read,
		platformv1.PlatformStaffService_RevokePlatformInvitation_FullMethodName:  write,

		platformv1.PlatformAccessService_GetMyPlatformAccess_FullMethodName:       middleware.UserMethod(),
		platformv1.PlatformAccessService_ListMyPlatformInvitations_FullMethodName: middleware.UserMethod(),
		platformv1.PlatformAccessService_AcceptPlatformInvitation_FullMethodName:  middleware.UserMethod(),
		platformv1.PlatformAccessService_DeclinePlatformInvitation_FullMethodName: middleware.UserMethod(),
	}
}

// ListPlatformPermissions returns the permission catalogue.
func (s *Server) ListPlatformPermissions(context.Context, *platformv1.ListPlatformPermissionsRequest) (*platformv1.ListPlatformPermissionsResponse, error) {
	catalogue := s.uc.PermissionCatalogue()
	resp := &platformv1.ListPlatformPermissionsResponse{Data: make([]*platformv1.PlatformPermissionInfo, 0, len(catalogue))}

	for _, e := range catalogue {
		resp.Data = append(resp.Data, &platformv1.PlatformPermissionInfo{
			Permission: permissionToProto(e.Permission), Module: e.Module, Action: e.Permission.Action(),
		})
	}

	return resp, nil
}

// ListPlatformRoles lists the roles.
func (s *Server) ListPlatformRoles(ctx context.Context, _ *platformv1.ListPlatformRolesRequest) (*platformv1.ListPlatformRolesResponse, error) {
	roles, err := s.uc.ListRoles(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &platformv1.ListPlatformRolesResponse{Data: make([]*platformv1.PlatformRole, 0, len(roles))}
	for _, r := range roles {
		resp.Data = append(resp.Data, roleToProto(r))
	}

	return resp, nil
}

// GetPlatformRole returns one role.
func (s *Server) GetPlatformRole(ctx context.Context, req *platformv1.GetPlatformRoleRequest) (*platformv1.GetPlatformRoleResponse, error) {
	role, err := s.uc.GetRole(ctx, req.GetRoleId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.GetPlatformRoleResponse{Role: roleToProto(role)}, nil
}

// CreatePlatformRole defines a role; answers 201.
func (s *Server) CreatePlatformRole(ctx context.Context, req *platformv1.CreatePlatformRoleRequest) (*platformv1.CreatePlatformRoleResponse, error) {
	fields, err := roleFields(req.GetRole(), []string{pathName, pathDescription, pathPermissions})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	role, err := s.uc.CreateRole(ctx, fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &platformv1.CreatePlatformRoleResponse{Role: roleToProto(role)}, nil
}

// UpdatePlatformRole changes the fields named by the update mask.
func (s *Server) UpdatePlatformRole(ctx context.Context, req *platformv1.UpdatePlatformRoleRequest) (*platformv1.UpdatePlatformRoleResponse, error) {
	fields, err := roleFields(req.GetRole(), maskPaths(req.GetUpdateMask().GetPaths(), pathName, pathDescription, pathPermissions))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	role, err := s.uc.UpdateRole(ctx, req.GetRoleId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.UpdatePlatformRoleResponse{Role: roleToProto(role)}, nil
}

// DeletePlatformRole removes an unused role.
func (s *Server) DeletePlatformRole(ctx context.Context, req *platformv1.DeletePlatformRoleRequest) (*platformv1.DeletePlatformRoleResponse, error) {
	if err := s.uc.DeleteRole(ctx, req.GetRoleId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.DeletePlatformRoleResponse{}, nil
}

// ListPlatformGroups lists the groups.
func (s *Server) ListPlatformGroups(ctx context.Context, _ *platformv1.ListPlatformGroupsRequest) (*platformv1.ListPlatformGroupsResponse, error) {
	groups, err := s.uc.ListGroups(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &platformv1.ListPlatformGroupsResponse{Data: make([]*platformv1.PlatformStaffGroup, 0, len(groups))}
	for _, g := range groups {
		resp.Data = append(resp.Data, groupToProto(g))
	}

	return resp, nil
}

// GetPlatformGroup returns one group with its members.
func (s *Server) GetPlatformGroup(ctx context.Context, req *platformv1.GetPlatformGroupRequest) (*platformv1.GetPlatformGroupResponse, error) {
	group, members, err := s.uc.GetGroup(ctx, req.GetGroupId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.GetPlatformGroupResponse{Group: groupToProto(group), Members: membersToProto(members)}, nil
}

// CreatePlatformGroup defines a group; answers 201.
func (s *Server) CreatePlatformGroup(ctx context.Context, req *platformv1.CreatePlatformGroupRequest) (*platformv1.CreatePlatformGroupResponse, error) {
	group, err := s.uc.CreateGroup(ctx, groupFields(req.GetGroup(), []string{pathName, pathDescription, pathRoleID}))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &platformv1.CreatePlatformGroupResponse{Group: groupToProto(group)}, nil
}

// UpdatePlatformGroup changes the fields named by the update mask.
func (s *Server) UpdatePlatformGroup(ctx context.Context, req *platformv1.UpdatePlatformGroupRequest) (*platformv1.UpdatePlatformGroupResponse, error) {
	paths := maskPaths(req.GetUpdateMask().GetPaths(), pathName, pathDescription, pathRoleID)

	for _, path := range paths {
		if path != pathName && path != pathDescription && path != pathRoleID {
			return nil, apperror.ToGRPCError(apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated"))
		}
	}

	group, err := s.uc.UpdateGroup(ctx, req.GetGroupId(), groupFields(req.GetGroup(), paths))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.UpdatePlatformGroupResponse{Group: groupToProto(group)}, nil
}

// DeletePlatformGroup removes a group with its members.
func (s *Server) DeletePlatformGroup(ctx context.Context, req *platformv1.DeletePlatformGroupRequest) (*platformv1.DeletePlatformGroupResponse, error) {
	if err := s.uc.DeleteGroup(ctx, req.GetGroupId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.DeletePlatformGroupResponse{}, nil
}

// ListPlatformStaff lists every user with platform access.
func (s *Server) ListPlatformStaff(ctx context.Context, _ *platformv1.ListPlatformStaffRequest) (*platformv1.ListPlatformStaffResponse, error) {
	staff, err := s.uc.ListStaff(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.ListPlatformStaffResponse{Data: membersToProto(staff)}, nil
}

// RemovePlatformStaffMember takes a user out of one group.
func (s *Server) RemovePlatformStaffMember(ctx context.Context, req *platformv1.RemovePlatformStaffMemberRequest) (*platformv1.RemovePlatformStaffMemberResponse, error) {
	if err := s.uc.RemoveMember(ctx, req.GetGroupId(), req.GetUserId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.RemovePlatformStaffMemberResponse{}, nil
}

// CreatePlatformInvitation invites a user into a group; answers 201.
func (s *Server) CreatePlatformInvitation(ctx context.Context, req *platformv1.CreatePlatformInvitationRequest) (*platformv1.CreatePlatformInvitationResponse, error) {
	inv, err := s.uc.CreateInvitation(ctx, req.GetEmail(), req.GetGroupId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &platformv1.CreatePlatformInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// ListPlatformInvitations lists the invitations, optionally by status.
func (s *Server) ListPlatformInvitations(ctx context.Context, req *platformv1.ListPlatformInvitationsRequest) (*platformv1.ListPlatformInvitationsResponse, error) {
	invitations, err := s.uc.ListInvitations(ctx, invitationStatusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.ListPlatformInvitationsResponse{Data: invitationsToProto(invitations)}, nil
}

// RevokePlatformInvitation cancels a pending invitation.
func (s *Server) RevokePlatformInvitation(ctx context.Context, req *platformv1.RevokePlatformInvitationRequest) (*platformv1.RevokePlatformInvitationResponse, error) {
	inv, err := s.uc.RevokeInvitation(ctx, req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.RevokePlatformInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// GetMyPlatformAccess returns what the caller may do on the platform.
func (s *Server) GetMyPlatformAccess(ctx context.Context, _ *platformv1.GetMyPlatformAccessRequest) (*platformv1.GetMyPlatformAccessResponse, error) {
	acc, pending, err := s.uc.MyAccess(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.GetMyPlatformAccessResponse{Access: &platformv1.MyPlatformAccess{
		PlatformAdmin:          acc.Admin,
		Permissions:            permissionsToProto(acc.Effective()),
		PendingInvitationCount: int32(pending), //nolint:gosec // a few invitations at most
	}}, nil
}

// ListMyPlatformInvitations lists the invitations sent to the caller.
func (s *Server) ListMyPlatformInvitations(ctx context.Context, req *platformv1.ListMyPlatformInvitationsRequest) (*platformv1.ListMyPlatformInvitationsResponse, error) {
	invitations, err := s.uc.ListMyInvitations(ctx, invitationStatusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.ListMyPlatformInvitationsResponse{Data: invitationsToProto(invitations)}, nil
}

// AcceptPlatformInvitation joins the group of the caller's invitation.
func (s *Server) AcceptPlatformInvitation(ctx context.Context, req *platformv1.AcceptPlatformInvitationRequest) (*platformv1.AcceptPlatformInvitationResponse, error) {
	inv, err := s.uc.AcceptInvitation(ctx, req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.AcceptPlatformInvitationResponse{Invitation: invitationToProto(inv)}, nil
}

// DeclinePlatformInvitation declines the caller's invitation.
func (s *Server) DeclinePlatformInvitation(ctx context.Context, req *platformv1.DeclinePlatformInvitationRequest) (*platformv1.DeclinePlatformInvitationResponse, error) {
	inv, err := s.uc.DeclineInvitation(ctx, req.GetInvitationId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &platformv1.DeclinePlatformInvitationResponse{Invitation: invitationToProto(inv)}, nil
}
