// Package client calls the public REST/JSON API (/api/v1) the way the web
// and mobile apps do: through the gateway, with a bearer token. Requests
// and responses are the vietgara-proto messages, encoded with protojson
// exactly as grpc-gateway expects, so the suite depends on the published
// contract and nothing else of the backend.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Client is bound to at most one access token; WithToken returns a client
// for another actor, so a test can hold several identities at once.
type Client struct {
	baseURL string
	http    *http.Client
	token   string
	headers map[string]string
}

// New returns an anonymous client for the API at baseURL (the gateway, without
// the /api suffix).
func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// WithToken returns a client that sends token as its bearer token.
func (c *Client) WithToken(token string) *Client {
	clone := *c
	clone.token = token

	return &clone
}

// WithHeader returns a client that also sends the header name: value, e.g.
// an Idempotency-Key.
func (c *Client) WithHeader(name, value string) *Client {
	clone := *c

	clone.headers = maps.Clone(c.headers)
	if clone.headers == nil {
		clone.headers = map[string]string{}
	}

	clone.headers[name] = value

	return &clone
}

// WithIdempotencyKey returns a client that sends key as its
// Idempotency-Key (API Specification Section 1.1).
func (c *Client) WithIdempotencyKey(key string) *Client { return c.WithHeader("Idempotency-Key", key) }

// APIError is a non-2xx response: the HTTP status plus the body's
// {"error": {"code", "message", "details"}}.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

// AsAPIError returns err as an *APIError, or nil.
func AsAPIError(err error) *APIError {
	apiErr, _ := errors.AsType[*APIError](err)

	return apiErr
}

var (
	marshal   = protojson.MarshalOptions{}
	unmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Do sends in (nil for no body) to method path and decodes the response
// into out (nil to ignore it).
func (c *Client) Do(ctx context.Context, method, path string, in, out proto.Message) error {
	req, err := c.newRequest(ctx, method, path, in)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(resp.StatusCode, raw)
	}

	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	if err := unmarshal.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decode %T: %w (body: %s)", method, path, out, err, raw)
	}

	return nil
}

// newRequest encodes in (nil for no body) and sets the client's headers.
func (c *Client) newRequest(ctx context.Context, method, path string, in proto.Message) (*http.Request, error) {
	var body io.Reader = http.NoBody

	if in != nil {
		raw, err := marshal.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("encode %s %s: %w", method, path, err)
		}

		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	for name, value := range c.headers {
		req.Header.Set(name, value)
	}

	return req, nil
}

func decodeError(status int, raw []byte) error {
	var body commonv1.ErrorResponse
	if err := unmarshal.Unmarshal(raw, &body); err != nil || body.GetError() == nil {
		return &APIError{Status: status, Message: strings.TrimSpace(string(raw))}
	}

	return &APIError{Status: status, Code: body.GetError().GetCode(), Message: body.GetError().GetMessage()}
}
