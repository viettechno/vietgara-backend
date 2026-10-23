package usecase

import (
	"context"
	"sort"

	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GroupFields are the settable group fields; nil fields are left unchanged.
type GroupFields struct {
	Name        *string
	Description *string
	RoleID      *string
}

// ListGroups returns every platform staff group.
func (uc *UseCase) ListGroups(ctx context.Context) ([]*domain.Group, error) {
	groups, err := uc.Repo.ListGroups(ctx)
	if err != nil {
		return nil, toAppError("list platform groups", err)
	}

	return groups, nil
}

// GetGroup returns a group with its members.
func (uc *UseCase) GetGroup(ctx context.Context, groupID string) (*domain.Group, []*domain.Member, error) {
	group, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return nil, nil, toAppError("get platform group", err)
	}

	ids, err := uc.Repo.GroupMembers(ctx, groupID)
	if err != nil {
		return nil, nil, toAppError("list group members", err)
	}

	users, err := uc.Users.ListByIDs(ctx, ids)
	if err != nil {
		return nil, nil, toAppError("load members", err)
	}

	members := make([]*domain.Member, 0, len(ids))

	for _, id := range ids {
		member := &domain.Member{UserID: id, Groups: []domain.GroupRef{{ID: group.ID, Name: group.Name}}}
		if user, ok := users[id]; ok {
			member.FullName, member.Email = user.FullName, user.Email
		}

		members = append(members, member)
	}

	return group, members, nil
}

// CreateGroup defines a new group with a role.
func (uc *UseCase) CreateGroup(ctx context.Context, fields GroupFields) (*domain.Group, error) {
	if fields.Name == nil {
		return nil, apperror.Validation("name", apperror.IssueRequired, "group name is required")
	}

	if fields.RoleID == nil || *fields.RoleID == "" {
		return nil, apperror.Validation("roleId", apperror.IssueRequired, "a group needs a role")
	}

	group := &domain.Group{}

	if err := uc.applyGroupFields(ctx, group, fields); err != nil {
		return nil, err
	}

	if err := uc.Repo.CreateGroup(ctx, group); err != nil {
		return nil, toAppError("create platform group", err)
	}

	created, err := uc.Repo.FindGroup(ctx, group.ID)
	if err != nil {
		return nil, toAppError("reload platform group", err)
	}

	if err := uc.audit(ctx, "platform_group.created", "platform_group", created.ID, nil, created); err != nil {
		return nil, toAppError("audit platform group", err)
	}

	return created, nil
}

// UpdateGroup changes a group; members get a changed role at once. A user
// who is not a Platform Admin cannot change a group whose role holds a
// permission they lack.
func (uc *UseCase) UpdateGroup(ctx context.Context, groupID string, fields GroupFields) (*domain.Group, error) {
	group, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return nil, toAppError("get platform group", err)
	}

	current, err := uc.Repo.FindRole(ctx, group.RoleID)
	if err != nil {
		return nil, toAppError("get platform role", err)
	}

	if err := requireHeld(ctx, current.Permissions); err != nil {
		return nil, err
	}

	before := *group

	if err := uc.applyGroupFields(ctx, group, fields); err != nil {
		return nil, err
	}

	if err := uc.Repo.UpdateGroup(ctx, group); err != nil {
		return nil, toAppError("update platform group", err)
	}

	updated, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return nil, toAppError("reload platform group", err)
	}

	if err := uc.audit(ctx, "platform_group.updated", "platform_group", groupID, before, updated); err != nil {
		return nil, toAppError("audit platform group", err)
	}

	return updated, nil
}

// DeleteGroup removes a group together with its memberships and pending
// invitations, so the members lose the group's permissions at once.
func (uc *UseCase) DeleteGroup(ctx context.Context, groupID string) error {
	group, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return toAppError("get platform group", err)
	}

	role, err := uc.Repo.FindRole(ctx, group.RoleID)
	if err != nil {
		return toAppError("get platform role", err)
	}

	if err := requireHeld(ctx, role.Permissions); err != nil {
		return err
	}

	members, err := uc.Repo.GroupMembers(ctx, groupID)
	if err != nil {
		return toAppError("list group members", err)
	}

	if err := uc.Repo.DeleteGroup(ctx, groupID); err != nil {
		return toAppError("delete platform group", err)
	}

	if err := uc.audit(ctx, "platform_group.deleted", "platform_group", groupID,
		map[string]any{"group": group, "members": members}, nil); err != nil {
		return toAppError("audit platform group", err)
	}

	return nil
}

func (uc *UseCase) applyGroupFields(ctx context.Context, group *domain.Group, fields GroupFields) error {
	if fields.Name != nil {
		if !domain.ValidName(*fields.Name, domain.MaxGroupNameLength) {
			return apperror.Validation("name", apperror.IssueInvalid, "group name must be 1 to 100 characters")
		}

		group.Name = *fields.Name
	}

	if fields.Description != nil {
		if len([]rune(*fields.Description)) > domain.MaxDescriptionSize {
			return apperror.Validation("description", apperror.IssueTooLong, "description is too long")
		}

		group.Description = *fields.Description
	}

	if fields.RoleID != nil {
		role, err := uc.Repo.FindRole(ctx, *fields.RoleID)
		if err != nil {
			return apperror.Wrap(apperror.CodeInvalidArgument, "role not found", err).
				WithField("roleId", apperror.IssueInvalid)
		}

		if err := requireHeld(ctx, role.Permissions); err != nil {
			return err
		}

		group.RoleID = role.ID
	}

	return nil
}

// ListStaff returns every user with platform access: the Platform Admins and
// the members of the groups, each with their groups.
func (uc *UseCase) ListStaff(ctx context.Context) ([]*domain.Member, error) {
	adminIDs, err := uc.Repo.AdminIDs(ctx)
	if err != nil {
		return nil, toAppError("list platform admins", err)
	}

	memberships, err := uc.Repo.Memberships(ctx)
	if err != nil {
		return nil, toAppError("list memberships", err)
	}

	ids := append([]string(nil), adminIDs...)

	for id := range memberships {
		if !containsID(ids, id) {
			ids = append(ids, id)
		}
	}

	users, err := uc.Users.ListByIDs(ctx, ids)
	if err != nil {
		return nil, toAppError("load staff", err)
	}

	staff := make([]*domain.Member, 0, len(ids))

	for _, id := range ids {
		member := &domain.Member{UserID: id, Admin: containsID(adminIDs, id), Groups: memberships[id]}
		if user, ok := users[id]; ok {
			member.FullName, member.Email = user.FullName, user.Email
		}

		staff = append(staff, member)
	}

	sort.SliceStable(staff, func(i, j int) bool {
		if staff[i].Admin != staff[j].Admin {
			return staff[i].Admin
		}

		return staff[i].FullName < staff[j].FullName
	})

	return staff, nil
}

// RemoveMember takes a user out of one group; their access from other
// groups stays. A user who is not a Platform Admin cannot remove a member of
// a group whose role holds a permission they lack.
func (uc *UseCase) RemoveMember(ctx context.Context, groupID, userID string) error {
	group, err := uc.Repo.FindGroup(ctx, groupID)
	if err != nil {
		return toAppError("get platform group", err)
	}

	role, err := uc.Repo.FindRole(ctx, group.RoleID)
	if err != nil {
		return toAppError("get platform role", err)
	}

	if err := requireHeld(ctx, role.Permissions); err != nil {
		return err
	}

	if err := uc.Repo.RemoveMember(ctx, groupID, userID); err != nil {
		return toAppError("remove member", err)
	}

	if err := uc.audit(ctx, "platform_staff.removed", "platform_group", groupID, map[string]string{"user_id": userID}, nil); err != nil {
		return toAppError("audit platform staff", err)
	}

	return nil
}
