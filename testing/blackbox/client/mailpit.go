package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Mailpit reads the e-mails the stubbed backend sends (MAIL_PROVIDER=smtp
// into Mailpit). Stubbed target only.
type Mailpit struct {
	baseURL string
	http    *http.Client
}

// NewMailpit returns a client for the Mailpit API at baseURL.
func NewMailpit(baseURL string) *Mailpit {
	return &Mailpit{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 10 * time.Second}}
}

// Message is a received e-mail.
type Message struct {
	Subject string `json:"Subject"`
	Text    string `json:"Text"`
}

var otpPattern = regexp.MustCompile(`\b(\d{6})\b`)

var (
	errNoOTPCode       = errors.New("no 6-digit code in")
	errMailpitResponse = errors.New("mailpit")
)

// OTP returns the 6-digit code in the body.
func (m *Message) OTP() (string, error) {
	match := otpPattern.FindStringSubmatch(m.Text)
	if match == nil {
		return "", fmt.Errorf("%w %q", errNoOTPCode, m.Subject)
	}

	return match[1], nil
}

// ErrNoMessage is returned when no e-mail arrives in time.
var ErrNoMessage = errors.New("no e-mail arrived")

// WaitForMessage returns the newest e-mail to address, polling until one
// arrives or ctx ends. Every user the suite creates has a unique
// address, so the newest message is the one the step under test sent.
func (m *Mailpit) WaitForMessage(ctx context.Context, address string) (*Message, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		id, err := m.newestID(ctx, address)
		if err != nil {
			return nil, err
		}

		if id != "" {
			var msg Message

			return &msg, m.getJSON(ctx, "/api/v1/message/"+url.PathEscape(id), &msg)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w for %s: %w", ErrNoMessage, address, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *Mailpit) newestID(ctx context.Context, address string) (string, error) {
	// Search results are newest first.
	var found struct {
		Messages []struct {
			ID string `json:"ID"`
		} `json:"messages"`
	}

	if err := m.getJSON(ctx, "/api/v1/search?limit=1&query="+url.QueryEscape("to:"+address), &found); err != nil {
		return "", err
	}

	if len(found.Messages) == 0 {
		return "", nil
	}

	return found.Messages[0].ID, nil
}

func (m *Mailpit) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("mailpit %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w %s: %s", errMailpitResponse, path, resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
