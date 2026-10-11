// Package mailpit provides the API operations needed to clean test-user messages.
package mailpit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pageSize = 100

type messageSummary struct {
	ID string `json:"ID"`
}

// Client deletes messages addressed to an explicit set of recipients.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// NewClient creates a client for the Mailpit API. Credentials must be either
// both set or both empty.
func NewClient(rawURL, username, password string) (*Client, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Mailpit URL: %w", err)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("mailpit URL must be an HTTP(S) origin without credentials, path, query, or fragment")
	}

	if (username == "") != (password == "") {
		return nil, errors.New("mailpit username and password must both be set or both be empty")
	}

	return &Client{
		baseURL:  strings.TrimSuffix(rawURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// DeleteMessagesForRecipients removes messages whose recipient exactly
// matches one of the supplied addresses. An empty list is a no-op.
func (c *Client) DeleteMessagesForRecipients(ctx context.Context, recipients []string) (int, error) {
	ids := make(map[string]struct{})

	for _, recipient := range recipients {
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient {
			return 0, fmt.Errorf("invalid Mailpit recipient %q", recipient)
		}

		for start := 0; ; start += pageSize {
			page, err := c.searchRecipient(ctx, recipient, start)
			if err != nil {
				return 0, fmt.Errorf("search Mailpit messages for %q: %w", recipient, err)
			}

			for _, message := range page {
				if message.ID == "" {
					return 0, errors.New("mailpit search returned a message without an ID")
				}

				ids[message.ID] = struct{}{}
			}

			if len(page) < pageSize {
				break
			}
		}
	}

	messageIDs := make([]string, 0, len(ids))
	for id := range ids {
		messageIDs = append(messageIDs, id)
	}

	sort.Strings(messageIDs)

	for start := 0; start < len(messageIDs); start += pageSize {
		end := min(start+pageSize, len(messageIDs))
		if err := c.deleteMessages(ctx, messageIDs[start:end]); err != nil {
			return 0, fmt.Errorf("delete Mailpit messages: %w", err)
		}
	}

	return len(messageIDs), nil
}

func (c *Client) searchRecipient(ctx context.Context, recipient string, start int) ([]messageSummary, error) {
	query := url.Values{
		"query": []string{"to:" + recipient},
		"limit": []string{strconv.Itoa(pageSize)},
		"start": []string{strconv.Itoa(start)},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/search?"+query.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search request returned %s", resp.Status)
	}

	var result struct {
		Messages []messageSummary `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	return result.Messages, nil
}

func (c *Client) deleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	body, err := json.Marshal(struct {
		IDs []string `json:"IDs"`
	}{IDs: ids})
	if err != nil {
		return fmt.Errorf("encode delete request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send delete request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("delete request returned %s (read response: %w)", resp.Status, readErr)
		}

		return fmt.Errorf("delete request returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}
