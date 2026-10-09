package usecase_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestRoles(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	ctx := e.as(t, owner, "")

	perms := []access.Permission{access.ReportRead, access.CustomerRead}
	role, err := e.UC.CreateRole(ctx, owner, usecase.RoleFields{Name: new(" Lễ tân "), Description: new(" Front desk "), Permissions: &perms})
	require.NoError(t, err)
	assert.Equal(t, "Lễ tân", role.Name)
	assert.Equal(t, []access.Permission{access.CustomerRead, access.ReportRead}, role.Permissions)

	_, err = e.UC.CreateRole(ctx, owner, usecase.RoleFields{Name: new("lễ tân")})
	requireReason(t, err, domain.ReasonRoleNameTaken)
	_, err = e.UC.CreateRole(ctx, owner, usecase.RoleFields{})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.UC.CreateRole(ctx, owner, usecase.RoleFields{Name: new("X"), Permissions: &[]access.Permission{"nope"}})
	requireReason(t, err, apperror.ReasonValidation)

	got, err := e.UC.GetRole(ctx, owner, role.ID)
	require.NoError(t, err)
	assert.Equal(t, role.ID, got.ID)

	_, err = e.UC.GetRole(ctx, staff, role.ID)
	requireReason(t, err, apperror.ReasonNotFound, "another owner's role")

	updated, err := e.UC.UpdateRole(ctx, owner, role.ID, usecase.RoleFields{Name: new("Reception")})
	require.NoError(t, err)
	assert.Equal(t, "Reception", updated.Name)

	_, err = e.UC.UpdateRole(ctx, owner, role.ID, usecase.RoleFields{Name: new("")})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.UC.UpdateRole(ctx, owner, "missing", usecase.RoleFields{})
	requireReason(t, err, apperror.ReasonNotFound)

	require.NoError(t, e.UC.DeleteRole(ctx, owner, role.ID))
	requireReason(t, e.UC.DeleteRole(ctx, owner, role.ID), apperror.ReasonNotFound)
	assert.Contains(t, e.Audit.Actions, "role.deleted")
}

func TestDefaultRolesConsumer(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	data, _ := json.Marshal(map[string]string{"user_id": staff, "locale": "vi"})

	require.NoError(t, e.UC.HandleUserEmailVerified(t.Context(), outbox.Event{Data: data}))
	require.NoError(t, e.UC.HandleUserEmailVerified(t.Context(), outbox.Event{Data: data}), "idempotent")
	require.NoError(t, e.UC.HandleUserEmailVerified(t.Context(), outbox.Event{Data: []byte("nope")}))

	roles, err := e.UC.ListRoles(t.Context(), staff)
	require.NoError(t, err)
	assert.Len(t, roles, 5)
	assert.Equal(t, "Cố vấn dịch vụ", roles[0].Name)

	e.Store.Fail["role.default"] = errBoom
	require.Error(t, e.UC.EnsureDefaultRoles(t.Context(), "someone", "vi"))

	e.Store.Fail["role.list"] = errBoom
	_, err = e.UC.ListRoles(t.Context(), staff)
	requireReason(t, err, apperror.ReasonInternal)
}

func TestGroups(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	roles, err := e.UC.ListRoles(ctx, owner)
	require.NoError(t, err)

	group, err := e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new("Ca tối"), Description: new("Night shift"), RoleID: &roles[0].ID})
	require.NoError(t, err)
	assert.Equal(t, roles[0].Name, group.RoleName)

	_, err = e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new("ca tối"), RoleID: &roles[0].ID})
	requireReason(t, err, domain.ReasonGroupNameTaken)
	_, err = e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new("X")})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new(""), RoleID: &roles[0].ID})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.UC.CreateGroup(ctx, id, usecase.GroupFields{Name: new("X"), RoleID: new("missing")})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.UC.CreateGroup(ctx, "missing", usecase.GroupFields{Name: new("X"), RoleID: &roles[0].ID})
	requireReason(t, err, apperror.ReasonNotFound)

	updated, err := e.UC.UpdateGroup(ctx, id, group.ID, usecase.GroupFields{Name: new("Ca đêm"), RoleID: &roles[1].ID})
	require.NoError(t, err)
	assert.Equal(t, roles[1].Name, updated.RoleName)

	_, err = e.UC.UpdateGroup(ctx, id, "missing", usecase.GroupFields{})
	requireReason(t, err, apperror.ReasonNotFound)
	_, err = e.UC.UpdateGroup(ctx, id, group.ID, usecase.GroupFields{Name: new(" ")})
	requireReason(t, err, apperror.ReasonValidation)

	requireReason(t, e.UC.DeleteRole(ctx, owner, roles[1].ID), domain.ReasonRoleInUse)

	require.NoError(t, e.UC.DeleteGroup(ctx, id, group.ID))
	requireReason(t, e.UC.DeleteGroup(ctx, id, group.ID), apperror.ReasonNotFound)

	e.Store.Fail["group.list"] = errBoom
	_, err = e.UC.ListGroups(ctx, id)
	requireReason(t, err, apperror.ReasonInternal)
}
