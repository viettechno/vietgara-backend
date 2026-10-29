package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
)

// GetNotificationSettings returns the garage's e-mail switches and sender
// name (FR-NOT-05).
func (c *Client) GetNotificationSettings(ctx context.Context, garageID string) (*notificationv1.NotificationSettings, error) {
	return call[notificationv1.NotificationSettings](ctx, c, http.MethodGet, garagePath(garageID)+"/notification-settings", nil)
}

// UpdateNotificationSettings replaces the sender name and switches off the
// types that are not enabled in types.
func (c *Client) UpdateNotificationSettings(
	ctx context.Context, garageID, senderName string, types ...*notificationv1.NotificationTypeSetting,
) (*notificationv1.NotificationSettings, error) {
	return call[notificationv1.NotificationSettings](ctx, c, http.MethodPut, garagePath(garageID)+"/notification-settings",
		&notificationv1.UpdateNotificationSettingsRequest{SenderName: senderName, Types: types})
}

// ListNotificationLog returns the garage's sends, newest first; the filters
// may be unspecified.
func (c *Client) ListNotificationLog(
	ctx context.Context, garageID string, typ notificationv1.NotificationType, status notificationv1.NotificationStatus,
) ([]*notificationv1.NotificationLogEntry, error) {
	query := url.Values{}
	if typ != notificationv1.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED {
		query.Set("type", typ.String())
	}

	if status != notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED {
		query.Set("status", status.String())
	}

	query.Set("pageSize", "100")

	resp, err := call[notificationv1.ListNotificationLogResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/notification-log?"+query.Encode(), nil)

	return resp.GetData(), err
}

// OptOutLinkToken returns the token of an opt-out link found in text.
func OptOutLinkToken(text string) string {
	const marker = "/api/v1/public/email-opt-out/"

	_, after, found := strings.Cut(text, marker)
	if !found {
		return ""
	}

	end := strings.IndexAny(after, " \r\n\t<>\"")
	if end < 0 {
		end = len(after)
	}

	return after[:end]
}

// OptOutPage opens the opt-out page of a token and answers the status and
// the body.
func (c *Client) OptOutPage(ctx context.Context, token string) (int, string, error) {
	return c.plain(ctx, http.MethodGet, "/api/v1/public/email-opt-out/"+url.PathEscape(token), "")
}

// OptOut presses the button of the opt-out page, as the page or a mail
// client's one-click request does.
func (c *Client) OptOut(ctx context.Context, token string) (int, string, error) {
	return c.plain(ctx, http.MethodPost, "/api/v1/public/email-opt-out/"+url.PathEscape(token), "List-Unsubscribe=One-Click")
}

// MailjetWebhook posts body to the provider's webhook with the given secret.
func (c *Client) MailjetWebhook(ctx context.Context, secret, body string) (int, error) {
	status, _, err := c.plain(ctx, http.MethodPost, "/api/v1/webhooks/mailjet/"+url.PathEscape(secret), body)

	return status, err
}

// plain sends a request that is not protojson and answers its status and body.
func (c *Client) plain(ctx context.Context, method, path, body string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)

	return resp.StatusCode, string(raw), err
}
