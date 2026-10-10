package usecase

import (
	"context"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GroupFields are the settable staff group fields.
type GroupFields struct {
	Name        *string
	Description *string
	RoleID      *string
}

// ListGroups returns a garage's staff groups.
func (uc *TenantUseCase) ListGroups(ctx context.Context, garageID string) ([]*domain.StaffGroup, error) {
	groups, err := uc.Groups.List(ctx, garageID)
	if err != nil {
		return nil, toAppError("list staff groups", err)
	}

	return groups, nil
}

// CreateGroup adds a staff group using one of the garage owner's roles.
func (uc *TenantUseCase) CreateGroup(ctx context.Context, garageID string, fields GroupFields) (*domain.StaffGroup, error) {
	if fields.Name == nil || fields.RoleID == nil {
		return nil, apperror.Validation("name", apperror.IssueRequired, "group name and role are required")
	}

	group := &domain.StaffGroup{GarageID: garageID}
	if err := uc.applyGroupFields(ctx, group, fields); err != nil {
		return nil, err
	}

	if err := uc.Groups.Create(ctx, group); err != nil {
		return nil, toAppError("create staff group", err)
	}

	if err := uc.audit(ctx, garageID, "staff_group.created", "staff_group", group.ID, nil, group); err != nil {
		return nil, toAppError("audit staff group", err)
	}

	return group, nil
}

// UpdateGroup renames a group or switches its role.
func (uc *TenantUseCase) UpdateGroup(ctx context.Context, garageID, groupID string, fields GroupFields) (*domain.StaffGroup, error) {
	group, err := uc.Groups.FindByID(ctx, garageID, groupID)
	if err != nil {
		return nil, toAppError("update staff group", err)
	}

	before := *group

	if err := uc.applyGroupFields(ctx, group, fields); err != nil {
		return nil, err
	}

	if err := uc.Groups.Update(ctx, group); err != nil {
		return nil, toAppError("update staff group", err)
	}

	if err := uc.audit(ctx, garageID, "staff_group.updated", "staff_group", group.ID, before, group); err != nil {
		return nil, toAppError("audit staff group", err)
	}

	return group, nil
}

// DeleteGroup removes a group; its members keep their other groups.
func (uc *TenantUseCase) DeleteGroup(ctx context.Context, garageID, groupID string) error {
	group, err := uc.Groups.FindByID(ctx, garageID, groupID)
	if err != nil {
		return toAppError("delete staff group", err)
	}

	if err := uc.Groups.Delete(ctx, garageID, groupID); err != nil {
		return toAppError("delete staff group", err)
	}

	if err := uc.audit(ctx, garageID, "staff_group.deleted", "staff_group", group.ID, group, nil); err != nil {
		return toAppError("audit staff group", err)
	}

	return nil
}

// ensureDefaultGroups gives a garage one group per default role of its
// owner.
func (uc *TenantUseCase) ensureDefaultGroups(ctx context.Context, garageID, ownerID string) error {
	roles, err := uc.Roles.ListByOwner(ctx, ownerID)
	if err != nil {
		return err
	}

	for _, role := range roles {
		if role.DefaultKey == nil {
			continue
		}

		group := &domain.StaffGroup{
			GarageID: garageID, Name: role.Name, Description: role.Description,
			RoleID: role.ID, DefaultKey: role.DefaultKey,
		}

		if _, err := uc.Groups.CreateDefault(ctx, group); err != nil {
			return err
		}
	}

	return nil
}

// applyGroupFields validates fields; the role must belong to the garage
// owner.
func (uc *TenantUseCase) applyGroupFields(ctx context.Context, group *domain.StaffGroup, f GroupFields) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if !domain.ValidRoleName(name, domain.MaxGroupNameLength) {
			return apperror.Validation("name", apperror.IssueRequired, "group name is required (at most 100 characters)")
		}

		group.Name = name
	}

	if f.Description != nil {
		group.Description = strings.TrimSpace(*f.Description)
	}

	if f.RoleID != nil {
		garage, err := uc.Garages.FindByID(ctx, group.GarageID)
		if err != nil {
			return toAppError("load garage", err)
		}

		role, err := uc.Roles.FindByID(ctx, *f.RoleID)
		if err != nil || role.OwnerUserID != garage.OwnerUserID {
			return apperror.Validation("roleId", apperror.IssueInvalid, "role not found")
		}

		group.RoleID = role.ID
		group.RoleName = role.Name
	}

	return nil
}
