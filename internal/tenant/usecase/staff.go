package usecase

import (
	"context"
	"slices"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// StaffFields are the settable membership fields; nil fields are left
// unchanged.
type StaffFields struct {
	Status   *domain.StaffStatus
	GroupIDs *[]string
}

// ListStaff returns the garage's members with user details, groups and
// effective permissions.
func (uc *TenantUseCase) ListStaff(ctx context.Context, garageID string) ([]*domain.Staff, error) {
	members, err := uc.Staff.List(ctx, garageID)
	if err != nil {
		return nil, toAppError("list staff", err)
	}

	if err := uc.withUsers(ctx, members...); err != nil {
		return nil, err
	}

	return members, nil
}

// UpdateStaff activates/deactivates a member or replaces their groups
// (FR-TEN-04). The owner's own membership cannot change; reactivating a
// member counts against the plan's staff limit.
func (uc *TenantUseCase) UpdateStaff(ctx context.Context, garageID, staffID string, fields StaffFields) (*domain.Staff, error) {
	member, err := uc.Staff.FindByID(ctx, garageID, staffID)
	if err != nil {
		return nil, toAppError("update staff", err)
	}

	if member.Owner {
		return nil, toAppError("update staff", domain.ErrOwnerMembership)
	}

	before := *member

	if fields.Status != nil && *fields.Status != member.Status {
		if err := uc.changeStaffStatus(ctx, garageID, member, *fields.Status); err != nil {
			return nil, err
		}
	}

	if fields.GroupIDs != nil {
		groupIDs, err := uc.validGroupIDs(ctx, garageID, *fields.GroupIDs)
		if err != nil {
			return nil, err
		}

		if err := uc.Staff.SetGroups(ctx, garageID, staffID, groupIDs); err != nil {
			return nil, toAppError("update staff groups", err)
		}
	}

	updated, err := uc.Staff.FindByID(ctx, garageID, staffID)
	if err != nil {
		return nil, toAppError("reload staff", err)
	}

	if err := uc.withUsers(ctx, updated); err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, garageID, "staff.updated", "staff", staffID, before, updated); err != nil {
		return nil, toAppError("audit staff", err)
	}

	return updated, nil
}

// changeStaffStatus (de)activates a member; reactivation counts against
// the plan's staff limit.
func (uc *TenantUseCase) changeStaffStatus(ctx context.Context, garageID string, member *domain.Staff, status domain.StaffStatus) error {
	if !status.IsValid() {
		return apperror.Validation("status", apperror.IssueInvalid, "status is invalid")
	}

	if status == domain.StaffStatusActive {
		if err := uc.checkGarageOwnerUsage(ctx, garageID, LimitStaff); err != nil {
			return err
		}
	}

	member.Status = status
	if err := uc.Staff.UpdateStatus(ctx, member); err != nil {
		return toAppError("update staff", err)
	}

	return nil
}

// DeleteStaff removes a member from the garage.
func (uc *TenantUseCase) DeleteStaff(ctx context.Context, garageID, staffID string) error {
	member, err := uc.Staff.FindByID(ctx, garageID, staffID)
	if err != nil {
		return toAppError("remove staff", err)
	}

	if member.Owner {
		return toAppError("remove staff", domain.ErrOwnerMembership)
	}

	if err := uc.Staff.Delete(ctx, garageID, staffID); err != nil {
		return toAppError("remove staff", err)
	}

	if err := uc.audit(ctx, garageID, "staff.removed", "staff", staffID, member, nil); err != nil {
		return toAppError("audit staff", err)
	}

	return uc.Events.Publish(ctx, EventStaffRemoved, garageID, member.UserID, map[string]any{
		keyGarageID: garageID, "user_id": member.UserID,
	})
}

// validGroupIDs de-duplicates ids and checks they are groups of the garage.
func (uc *TenantUseCase) validGroupIDs(ctx context.Context, garageID string, ids []string) ([]string, error) {
	groups, err := uc.Groups.List(ctx, garageID)
	if err != nil {
		return nil, toAppError("list staff groups", err)
	}

	valid := make([]string, 0, len(ids))

	for _, id := range ids {
		if !slices.ContainsFunc(groups, func(g *domain.StaffGroup) bool { return g.ID == id }) {
			return nil, apperror.Validation("groupIds", apperror.IssueInvalid, "unknown staff group "+id)
		}

		if !slices.Contains(valid, id) {
			valid = append(valid, id)
		}
	}

	return valid, nil
}

// checkGarageOwnerUsage checks limit for the owner of garageID.
func (uc *TenantUseCase) checkGarageOwnerUsage(ctx context.Context, garageID string, limit Limit) error {
	garage, err := uc.Garages.FindByID(ctx, garageID)
	if err != nil {
		return toAppError("load garage", err)
	}

	return uc.checkUsage(ctx, garage.OwnerUserID, limit)
}

// CheckGarageOwnerUsageForTest is a test helper for checkGarageOwnerUsage.
func (uc *TenantUseCase) CheckGarageOwnerUsageForTest(ctx context.Context, garageID string, limit Limit) error {
	return uc.checkGarageOwnerUsage(ctx, garageID, limit)
}
