package usecase

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// RoleFields are the settable role fields; nil fields are left unchanged.
type RoleFields struct {
	Name        *string
	Description *string
	Permissions *[]access.Permission
}

// ListRoles returns the owner's roles.
func (uc *TenantUseCase) ListRoles(ctx context.Context, ownerID string) ([]*domain.Role, error) {
	roles, err := uc.Roles.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, toAppError("list roles", err)
	}

	return roles, nil
}

// GetRole returns one of the owner's roles.
func (uc *TenantUseCase) GetRole(ctx context.Context, ownerID, roleID string) (*domain.Role, error) {
	role, err := uc.Roles.FindByID(ctx, roleID)
	if err != nil {
		return nil, toAppError("get role", err)
	}

	if role.OwnerAccountID != ownerID {
		return nil, toAppError("get role", domain.ErrRoleNotFound)
	}

	return role, nil
}

// CreateRole defines a new role for the owner's garages.
func (uc *TenantUseCase) CreateRole(ctx context.Context, ownerID string, fields RoleFields) (*domain.Role, error) {
	role := &domain.Role{OwnerAccountID: ownerID, Permissions: []access.Permission{}}

	if fields.Name == nil {
		return nil, apperror.Validation("name", apperror.IssueRequired, "role name is required")
	}

	if err := applyRoleFields(role, fields); err != nil {
		return nil, err
	}

	if err := uc.Roles.Create(ctx, role); err != nil {
		return nil, toAppError("create role", err)
	}

	if err := uc.audit(ctx, "", "role.created", "role", role.ID, nil, role); err != nil {
		return nil, toAppError("audit role", err)
	}

	return role, nil
}

// UpdateRole changes one of the owner's roles; every group using it gets
// the new permissions at once.
func (uc *TenantUseCase) UpdateRole(ctx context.Context, ownerID, roleID string, fields RoleFields) (*domain.Role, error) {
	role, err := uc.GetRole(ctx, ownerID, roleID)
	if err != nil {
		return nil, err
	}

	before := *role

	if err := applyRoleFields(role, fields); err != nil {
		return nil, err
	}

	if err := uc.Roles.Update(ctx, role); err != nil {
		return nil, toAppError("update role", err)
	}

	if err := uc.audit(ctx, "", "role.updated", "role", role.ID, before, role); err != nil {
		return nil, toAppError("audit role", err)
	}

	return role, nil
}

// DeleteRole removes a role no staff group uses.
func (uc *TenantUseCase) DeleteRole(ctx context.Context, ownerID, roleID string) error {
	role, err := uc.GetRole(ctx, ownerID, roleID)
	if err != nil {
		return err
	}

	if err := uc.Roles.Delete(ctx, roleID); err != nil {
		return toAppError("delete role", err)
	}

	if err := uc.audit(ctx, "", "role.deleted", "role", role.ID, role, nil); err != nil {
		return toAppError("audit role", err)
	}

	return nil
}

// EnsureDefaultRoles creates the owner's default roles (named in locale)
// unless they exist. Idempotent: called when the owner's e-mail is verified
// and again before their first garage is created.
func (uc *TenantUseCase) EnsureDefaultRoles(ctx context.Context, ownerID, locale string) error {
	return uc.Tx.WithTransaction(ctx, platformdb.AccountScope(ownerID), func(ctx context.Context) error {
		for _, tmpl := range domain.DefaultRoleTemplates {
			name, description := tmpl.Localized(locale)
			key := tmpl.Key
			role := &domain.Role{
				OwnerAccountID: ownerID, Name: name, Description: description,
				Permissions: tmpl.Permissions, DefaultKey: &key,
			}

			if _, err := uc.Roles.CreateDefault(ctx, role); err != nil {
				return err
			}
		}

		return nil
	})
}

// HandleAccountEmailVerified is the account.email_verified consumer: a
// newly verified owner gets the default roles (FR-IAM-03).
func (uc *TenantUseCase) HandleAccountEmailVerified(ctx context.Context, event outbox.Event) error {
	var payload struct {
		AccountID string `json:"account_id"`
		Locale    string `json:"locale"`
	}

	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.AccountID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.EnsureDefaultRoles(ctx, payload.AccountID, payload.Locale)
}

func applyRoleFields(role *domain.Role, f RoleFields) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if !domain.ValidRoleName(name, domain.MaxRoleNameLength) {
			return apperror.Validation("name", apperror.IssueRequired, "role name is required (at most 100 characters)")
		}

		role.Name = name
	}

	if f.Description != nil {
		role.Description = strings.TrimSpace(*f.Description)
	}

	if f.Permissions != nil {
		for _, p := range *f.Permissions {
			if !p.IsValid() {
				return apperror.Validation("permissions", apperror.IssueInvalid, "unknown permission "+string(p))
			}
		}

		role.Permissions = access.Normalize(*f.Permissions)
	}

	return nil
}
