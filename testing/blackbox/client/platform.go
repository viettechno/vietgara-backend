package client

import (
	"context"
	"net/http"
	"net/url"

	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"
)

// ListPlatformPermissions returns the platform permission catalogue.
func (c *Client) ListPlatformPermissions(ctx context.Context) ([]*platformv1.PlatformPermissionInfo, error) {
	resp, err := call[platformv1.ListPlatformPermissionsResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-permissions", nil)

	return resp.GetData(), err
}

// ListPlatformRoles returns the platform roles.
func (c *Client) ListPlatformRoles(ctx context.Context) ([]*platformv1.PlatformRole, error) {
	resp, err := call[platformv1.ListPlatformRolesResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-roles", nil)

	return resp.GetData(), err
}

// GetPlatformRole returns one platform role.
func (c *Client) GetPlatformRole(ctx context.Context, roleID string) (*platformv1.PlatformRole, error) {
	return call[platformv1.PlatformRole](ctx, c, http.MethodGet, "/api/v1/admin/platform-roles/"+url.PathEscape(roleID), nil)
}

// CreatePlatformRole defines a platform role.
func (c *Client) CreatePlatformRole(ctx context.Context, role *platformv1.PlatformRole) (*platformv1.PlatformRole, error) {
	return call[platformv1.PlatformRole](ctx, c, http.MethodPost, "/api/v1/admin/platform-roles", role)
}

// UpdatePlatformRole patches a platform role (the whole body is applied unless paths narrow it).
func (c *Client) UpdatePlatformRole(ctx context.Context, roleID string, role *platformv1.PlatformRole) (*platformv1.PlatformRole, error) {
	return call[platformv1.PlatformRole](ctx, c, http.MethodPatch, "/api/v1/admin/platform-roles/"+url.PathEscape(roleID), role)
}

// DeletePlatformRole removes a platform role no group uses.
func (c *Client) DeletePlatformRole(ctx context.Context, roleID string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/admin/platform-roles/"+url.PathEscape(roleID), nil, nil)
}

// ListPlatformGroups returns the platform staff groups.
func (c *Client) ListPlatformGroups(ctx context.Context) ([]*platformv1.PlatformStaffGroup, error) {
	resp, err := call[platformv1.ListPlatformGroupsResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-groups", nil)

	return resp.GetData(), err
}

// GetPlatformGroup returns one platform staff group with its members.
func (c *Client) GetPlatformGroup(ctx context.Context, groupID string) (*platformv1.GetPlatformGroupResponse, error) {
	return call[platformv1.GetPlatformGroupResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-groups/"+url.PathEscape(groupID), nil)
}

// CreatePlatformGroup defines a platform staff group.
func (c *Client) CreatePlatformGroup(ctx context.Context, group *platformv1.PlatformStaffGroup) (*platformv1.PlatformStaffGroup, error) {
	return call[platformv1.PlatformStaffGroup](ctx, c, http.MethodPost, "/api/v1/admin/platform-groups", group)
}

// UpdatePlatformGroup patches a platform staff group.
func (c *Client) UpdatePlatformGroup(ctx context.Context, groupID string, group *platformv1.PlatformStaffGroup) (*platformv1.PlatformStaffGroup, error) {
	return call[platformv1.PlatformStaffGroup](ctx, c, http.MethodPatch, "/api/v1/admin/platform-groups/"+url.PathEscape(groupID), group)
}

// DeletePlatformGroup removes a platform staff group with its members.
func (c *Client) DeletePlatformGroup(ctx context.Context, groupID string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/admin/platform-groups/"+url.PathEscape(groupID), nil, nil)
}

// ListPlatformStaff returns the users with platform access.
func (c *Client) ListPlatformStaff(ctx context.Context) ([]*platformv1.PlatformStaffMember, error) {
	resp, err := call[platformv1.ListPlatformStaffResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-staff", nil)

	return resp.GetData(), err
}

// RemovePlatformStaffMember takes a user out of one platform staff group.
func (c *Client) RemovePlatformStaffMember(ctx context.Context, groupID, userID string) error {
	return c.Do(ctx, http.MethodDelete,
		"/api/v1/admin/platform-groups/"+url.PathEscape(groupID)+"/members/"+url.PathEscape(userID), nil, nil)
}

// CreatePlatformInvitation invites an existing user into a platform staff group.
func (c *Client) CreatePlatformInvitation(ctx context.Context, email, groupID string) (*platformv1.PlatformInvitation, error) {
	return call[platformv1.PlatformInvitation](ctx, c, http.MethodPost, "/api/v1/admin/platform-invitations",
		&platformv1.CreatePlatformInvitationRequest{Email: email, GroupId: groupID})
}

// ListPlatformInvitations returns the platform invitations.
func (c *Client) ListPlatformInvitations(ctx context.Context) ([]*platformv1.PlatformInvitation, error) {
	resp, err := call[platformv1.ListPlatformInvitationsResponse](ctx, c, http.MethodGet, "/api/v1/admin/platform-invitations", nil)

	return resp.GetData(), err
}

// RevokePlatformInvitation cancels a pending platform invitation.
func (c *Client) RevokePlatformInvitation(ctx context.Context, invitationID string) (*platformv1.PlatformInvitation, error) {
	return call[platformv1.PlatformInvitation](ctx, c, http.MethodPost,
		"/api/v1/admin/platform-invitations/"+url.PathEscape(invitationID)+"/revoke", &platformv1.RevokePlatformInvitationRequest{})
}

// GetMyPlatformAccess returns what the caller may do on the platform.
func (c *Client) GetMyPlatformAccess(ctx context.Context) (*platformv1.MyPlatformAccess, error) {
	return call[platformv1.MyPlatformAccess](ctx, c, http.MethodGet, "/api/v1/admin/me/permissions", nil)
}

// ListMyPlatformInvitations returns the platform invitations sent to the caller.
func (c *Client) ListMyPlatformInvitations(ctx context.Context) ([]*platformv1.PlatformInvitation, error) {
	resp, err := call[platformv1.ListMyPlatformInvitationsResponse](ctx, c, http.MethodGet, "/api/v1/me/platform-invitations", nil)

	return resp.GetData(), err
}

// AcceptPlatformInvitation joins the group of the caller's invitation.
func (c *Client) AcceptPlatformInvitation(ctx context.Context, invitationID string) (*platformv1.PlatformInvitation, error) {
	return call[platformv1.PlatformInvitation](ctx, c, http.MethodPost,
		"/api/v1/me/platform-invitations/"+url.PathEscape(invitationID)+"/accept", &platformv1.AcceptPlatformInvitationRequest{})
}

// DeclinePlatformInvitation declines the caller's invitation.
func (c *Client) DeclinePlatformInvitation(ctx context.Context, invitationID string) (*platformv1.PlatformInvitation, error) {
	return call[platformv1.PlatformInvitation](ctx, c, http.MethodPost,
		"/api/v1/me/platform-invitations/"+url.PathEscape(invitationID)+"/decline", &platformv1.DeclinePlatformInvitationRequest{})
}
