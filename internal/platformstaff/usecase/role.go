package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// RoleFields are the settable role fields; nil fields are left unchanged.
type RoleFields struct {
	Name        *string
	Description *string
	Permissions *[]access.PlatformPermission
}

// ListRoles returns every platform role.
func (uc *UseCase) ListRoles(ctx context.Context) ([]*domain.Role, error) {
	roles, err := uc.Repo.ListRoles(ctx)
	if err != nil {
		return nil, toAppError("list platform roles", err)
	}

	return roles, nil
}

// GetRole returns one role.
func (uc *UseCase) GetRole(ctx context.Context, roleID string) (*domain.Role, error) {
	role, err := uc.Repo.FindRole(ctx, roleID)
	if err != nil {
		return nil, toAppError("get platform role", err)
	}

	return role, nil
}

// CreateRole defines a new role.
func (uc *UseCase) CreateRole(ctx context.Context, fields RoleFields) (*domain.Role, error) {
	if fields.Name == nil {
		return nil, apperror.Validation("name", apperror.IssueRequired, "role name is required")
	}

	role := &domain.Role{Permissions: []access.PlatformPermission{}}

	if err := applyRoleFields(ctx, role, fields); err != nil {
		return nil, err
	}

	if err := uc.Repo.CreateRole(ctx, role); err != nil {
		return nil, toAppError("create platform role", err)
	}

	if err := uc.audit(ctx, "platform_role.created", "platform_role", role.ID, nil, role); err != nil {
		return nil, toAppError("audit platform role", err)
	}

	return role, nil
}

// UpdateRole changes a role; every group using it gets the new permissions
// at once. A user who is not a Platform Admin cannot change a role that
// holds a permission they lack, whatever the change, or they could strip it
// from the people who hold it.
func (uc *UseCase) UpdateRole(ctx context.Context, roleID string, fields RoleFields) (*domain.Role, error) {
	role, err := uc.GetRole(ctx, roleID)
	if err != nil {
		return nil, err
	}

	if err := requireHeld(ctx, role.Permissions); err != nil {
		return nil, err
	}

	before := *role

	if err := applyRoleFields(ctx, role, fields); err != nil {
		return nil, err
	}

	if err := uc.Repo.UpdateRole(ctx, role); err != nil {
		return nil, toAppError("update platform role", err)
	}

	if err := uc.audit(ctx, "platform_role.updated", "platform_role", role.ID, before, role); err != nil {
		return nil, toAppError("audit platform role", err)
	}

	return role, nil
}

// DeleteRole removes a role no group uses.
func (uc *UseCase) DeleteRole(ctx context.Context, roleID string) error {
	role, err := uc.GetRole(ctx, roleID)
	if err != nil {
		return err
	}

	if err := uc.Repo.DeleteRole(ctx, roleID); err != nil {
		return toAppError("delete platform role", err)
	}

	if err := uc.audit(ctx, "platform_role.deleted", "platform_role", role.ID, role, nil); err != nil {
		return toAppError("audit platform role", err)
	}

	return nil
}

func applyRoleFields(ctx context.Context, role *domain.Role, fields RoleFields) error {
	if fields.Name != nil {
		if !domain.ValidName(*fields.Name, domain.MaxRoleNameLength) {
			return apperror.Validation("name", apperror.IssueInvalid, "role name must be 1 to 100 characters")
		}

		role.Name = *fields.Name
	}

	if fields.Description != nil {
		if len([]rune(*fields.Description)) > domain.MaxDescriptionSize {
			return apperror.Validation("description", apperror.IssueTooLong, "description is too long")
		}

		role.Description = *fields.Description
	}

	if fields.Permissions != nil {
		for _, p := range *fields.Permissions {
			if !p.IsValid() {
				return apperror.Validation("permissions", apperror.IssueInvalid, "unknown permission")
			}
		}

		permissions := access.NormalizePlatform(*fields.Permissions)
		if err := requireHeld(ctx, permissions); err != nil {
			return err
		}

		role.Permissions = permissions
	}

	return nil
}
