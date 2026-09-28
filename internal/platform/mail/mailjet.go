package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultMailjetEndpoint is the Mailjet Send API v3.1.
const DefaultMailjetEndpoint = "https://api.mailjet.com/v3.1/send"

// MailjetSender delivers through the Mailjet Send API v3.1, authenticated
// with an API key and secret (HTTP basic auth). See the backend README.
type MailjetSender struct {
	apiKey    string
	secretKey string
	from      From
	endpoint  string
	client    *http.Client
}

// NewMailjetSender returns a MailjetSender; endpoint defaults to
// DefaultMailjetEndpoint.
func NewMailjetSender(apiKey, secretKey string, from From, endpoint string) *MailjetSender {
	if endpoint == "" {
		endpoint = DefaultMailjetEndpoint
	}

	return &MailjetSender{
		apiKey: apiKey, secretKey: secretKey, from: from, endpoint: endpoint,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type mailjetAddress struct {
	Email string `json:"Email"`
	Name  string `json:"Name,omitempty"`
}

type mailjetMessage struct {
	From     mailjetAddress   `json:"From"`
	To       []mailjetAddress `json:"To"`
	Subject  string           `json:"Subject"`
	TextPart string           `json:"TextPart,omitempty"`
	HTMLPart string           `json:"HTMLPart,omitempty"`
	CustomID string           `json:"CustomID,omitempty"`
}

type mailjetRequest struct {
	Messages []mailjetMessage `json:"Messages"`
}

type mailjetResponse struct {
	Messages []struct {
		Status string `json:"Status"`
		Errors []struct {
			ErrorMessage string `json:"ErrorMessage"`
		} `json:"Errors"`
	} `json:"Messages"`
}

// Send delivers msg; a non-2xx answer or a message status other than
// "success" is an error.
func (s *MailjetSender) Send(ctx context.Context, msg Message) error {
	if err := validate(msg); err != nil {
		return err
	}

	body, err := json.Marshal(mailjetRequest{Messages: []mailjetMessage{{
		From:     mailjetAddress{Email: s.from.Email, Name: s.from.Name},
		To:       []mailjetAddress{{Email: msg.ToEmail, Name: msg.ToName}},
		Subject:  msg.Subject,
		TextPart: msg.Text,
		HTMLPart: msg.HTML,
		CustomID: msg.Tag,
	}}})
	if err != nil {
		return fmt.Errorf("mailjet: encode: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailjet: request: %w", err)
	}

	req.SetBasicAuth(s.apiKey, s.secretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("mailjet: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mailjet: status %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}

	var parsed mailjetResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("mailjet: decode response: %w", err)
	}

	for _, m := range parsed.Messages {
		if m.Status != "success" {
			reason := m.Status
			if len(m.Errors) > 0 {
				reason = m.Errors[0].ErrorMessage
			}

			return fmt.Errorf("mailjet: message rejected: %s", reason)
		}
	}

	return nil
}
