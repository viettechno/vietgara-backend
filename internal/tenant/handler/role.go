package handler

import (
	"context"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const pathDescription = "description"

// ListPermissions returns the permission catalog.
func (s *Server) ListPermissions(context.Context, *tenantv1.ListPermissionsRequest) (*tenantv1.ListPermissionsResponse, error) {
	resp := &tenantv1.ListPermissionsResponse{Data: make([]*tenantv1.PermissionInfo, 0, len(access.Catalog))}
	for _, e := range access.Catalog {
		resp.Data = append(resp.Data, &tenantv1.PermissionInfo{
			Permission: access.ToProto(e.Permission), Module: e.Module, Action: e.Permission.Action(),
		})
	}

	return resp, nil
}

// ListRoles lists the caller's roles.
func (s *Server) ListRoles(ctx context.Context, _ *tenantv1.ListRolesRequest) (*tenantv1.ListRolesResponse, error) {
	roles, err := s.uc.ListRoles(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListRolesResponse{Data: make([]*tenantv1.Role, 0, len(roles))}
	for _, r := range roles {
		resp.Data = append(resp.Data, roleToProto(r))
	}

	return resp, nil
}

// GetRole returns one of the caller's roles.
func (s *Server) GetRole(ctx context.Context, req *tenantv1.GetRoleRequest) (*tenantv1.GetRoleResponse, error) {
	role, err := s.uc.GetRole(ctx, middleware.AccountID(ctx), req.GetRoleId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.GetRoleResponse{Role: roleToProto(role)}, nil
}

// CreateRole defines a role; answers 201.
func (s *Server) CreateRole(ctx context.Context, req *tenantv1.CreateRoleRequest) (*tenantv1.CreateRoleResponse, error) {
	fields, err := roleFields(req.GetRole(), []string{pathName, pathDescription, "permissions"})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	role, err := s.uc.CreateRole(ctx, middleware.AccountID(ctx), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &tenantv1.CreateRoleResponse{Role: roleToProto(role)}, nil
}

// UpdateRole changes the fields named by the update mask.
func (s *Server) UpdateRole(ctx context.Context, req *tenantv1.UpdateRoleRequest) (*tenantv1.UpdateRoleResponse, error) {
	fields, err := roleFields(req.GetRole(), maskPaths(req.GetUpdateMask().GetPaths(), pathName, pathDescription, "permissions"))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	role, err := s.uc.UpdateRole(ctx, middleware.AccountID(ctx), req.GetRoleId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UpdateRoleResponse{Role: roleToProto(role)}, nil
}

// DeleteRole removes an unused role.
func (s *Server) DeleteRole(ctx context.Context, req *tenantv1.DeleteRoleRequest) (*tenantv1.DeleteRoleResponse, error) {
	if err := s.uc.DeleteRole(ctx, middleware.AccountID(ctx), req.GetRoleId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.DeleteRoleResponse{}, nil
}

func roleFields(in *tenantv1.Role, paths []string) (usecase.RoleFields, error) {
	var f usecase.RoleFields

	for _, path := range paths {
		switch path {
		case pathName:
			f.Name = new(in.GetName())
		case pathDescription:
			f.Description = new(in.GetDescription())
		case "permissions":
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

// ListStaffGroups lists the garage's staff groups.
func (s *Server) ListStaffGroups(ctx context.Context, req *tenantv1.ListStaffGroupsRequest) (*tenantv1.ListStaffGroupsResponse, error) {
	groups, err := s.uc.ListGroups(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListStaffGroupsResponse{Data: make([]*tenantv1.StaffGroup, 0, len(groups))}
	for _, g := range groups {
		resp.Data = append(resp.Data, groupToProto(g))
	}

	return resp, nil
}

// CreateStaffGroup adds a group; answers 201.
func (s *Server) CreateStaffGroup(ctx context.Context, req *tenantv1.CreateStaffGroupRequest) (*tenantv1.CreateStaffGroupResponse, error) {
	fields, err := groupFields(req.GetGroup(), []string{pathName, pathDescription, "role_id"})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	group, err := s.uc.CreateGroup(ctx, req.GetGarageId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &tenantv1.CreateStaffGroupResponse{Group: groupToProto(group)}, nil
}

// UpdateStaffGroup changes the fields named by the update mask.
func (s *Server) UpdateStaffGroup(ctx context.Context, req *tenantv1.UpdateStaffGroupRequest) (*tenantv1.UpdateStaffGroupResponse, error) {
	fields, err := groupFields(req.GetGroup(), maskPaths(req.GetUpdateMask().GetPaths(), pathName, pathDescription, "role_id"))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	group, err := s.uc.UpdateGroup(ctx, req.GetGarageId(), req.GetGroupId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UpdateStaffGroupResponse{Group: groupToProto(group)}, nil
}

// DeleteStaffGroup removes a group.
func (s *Server) DeleteStaffGroup(ctx context.Context, req *tenantv1.DeleteStaffGroupRequest) (*tenantv1.DeleteStaffGroupResponse, error) {
	if err := s.uc.DeleteGroup(ctx, req.GetGarageId(), req.GetGroupId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.DeleteStaffGroupResponse{}, nil
}

func groupFields(in *tenantv1.StaffGroup, paths []string) (usecase.GroupFields, error) {
	var f usecase.GroupFields

	for _, path := range paths {
		switch path {
		case pathName:
			f.Name = new(in.GetName())
		case pathDescription:
			f.Description = new(in.GetDescription())
		case "role_id":
			f.RoleID = new(in.GetRoleId())
		default:
			return f, apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
		}
	}

	return f, nil
}
