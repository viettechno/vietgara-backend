package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"
)

func supportGrantPath(garageID, grantID string) string {
	return garagePath(garageID) + "/support-grants/" + url.PathEscape(grantID)
}

// ListMySupportSessions returns the garages the caller can read now.
func (c *Client) ListMySupportSessions(ctx context.Context) ([]*supportv1.SupportGrant, error) {
	resp, err := call[supportv1.ListMySupportSessionsResponse](ctx, c, http.MethodGet, "/api/v1/me/support-sessions", nil)

	return resp.GetData(), err
}

// ListGarageDirectory searches garages by name and owner (admin staff).
func (c *Client) ListGarageDirectory(ctx context.Context, query string) ([]*supportv1.GarageDirectoryEntry, error) {
	resp, err := call[supportv1.ListGarageDirectoryResponse](ctx, c, http.MethodGet, "/api/v1/admin/garages?query="+url.QueryEscape(query), nil)

	return resp.GetData(), err
}

// RequestSupportGrant asks for read-only access to a garage.
func (c *Client) RequestSupportGrant(ctx context.Context, garageID, reason string, ttlHours int32) (*supportv1.SupportGrant, error) {
	return call[supportv1.SupportGrant](ctx, c, http.MethodPost, "/api/v1/admin/support-grants",
		&supportv1.RequestSupportGrantRequest{GarageId: garageID, Reason: reason, TtlHours: ttlHours})
}

// ListSupportGrants returns the caller's own grants.
func (c *Client) ListSupportGrants(ctx context.Context) ([]*supportv1.SupportGrant, error) {
	resp, err := call[supportv1.ListSupportGrantsResponse](ctx, c, http.MethodGet, "/api/v1/admin/support-grants", nil)

	return resp.GetData(), err
}

// EndSupportGrant closes the caller's own grant.
func (c *Client) EndSupportGrant(ctx context.Context, grantID string) (*supportv1.SupportGrant, error) {
	return call[supportv1.SupportGrant](ctx, c, http.MethodPost, "/api/v1/admin/support-grants/"+url.PathEscape(grantID)+":end",
		&supportv1.EndSupportGrantRequest{})
}

// ListSupportOutboxEvents shows the delivery status of the grant's garage's events.
func (c *Client) ListSupportOutboxEvents(ctx context.Context, grantID string) ([]*supportv1.SupportOutboxEvent, error) {
	resp, err := call[supportv1.ListSupportOutboxEventsResponse](ctx, c, http.MethodGet,
		"/api/v1/admin/support-grants/"+url.PathEscape(grantID)+"/outbox-events", nil)

	return resp.GetData(), err
}

// ListSupportNotifications shows the notification log of the grant's garage's people.
func (c *Client) ListSupportNotifications(ctx context.Context, grantID string) ([]*supportv1.SupportNotification, error) {
	resp, err := call[supportv1.ListSupportNotificationsResponse](ctx, c, http.MethodGet,
		"/api/v1/admin/support-grants/"+url.PathEscape(grantID)+"/notifications", nil)

	return resp.GetData(), err
}

// ListGarageSupportGrants returns the garage's grants (owner).
func (c *Client) ListGarageSupportGrants(ctx context.Context, garageID string) ([]*supportv1.SupportGrant, error) {
	resp, err := call[supportv1.ListGarageSupportGrantsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/support-grants", nil)

	return resp.GetData(), err
}

// ApproveSupportGrant approves a pending request (owner).
func (c *Client) ApproveSupportGrant(ctx context.Context, garageID, grantID string) (*supportv1.SupportGrant, error) {
	return call[supportv1.SupportGrant](ctx, c, http.MethodPost, supportGrantPath(garageID, grantID)+":approve",
		&supportv1.ApproveSupportGrantRequest{})
}

// RevokeSupportGrant ends or declines a grant (owner).
func (c *Client) RevokeSupportGrant(ctx context.Context, garageID, grantID string) (*supportv1.SupportGrant, error) {
	return call[supportv1.SupportGrant](ctx, c, http.MethodPost, supportGrantPath(garageID, grantID)+":revoke",
		&supportv1.RevokeSupportGrantRequest{})
}

// ListSupportAccessLog returns what support read in the garage (owner).
func (c *Client) ListSupportAccessLog(ctx context.Context, garageID string, pageSize int) ([]*supportv1.SupportAccessEvent, error) {
	resp, err := call[supportv1.ListSupportAccessLogResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/support-access-log?pageSize="+strconv.Itoa(pageSize), nil)

	return resp.GetData(), err
}
