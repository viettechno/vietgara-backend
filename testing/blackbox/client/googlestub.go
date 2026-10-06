package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// GoogleStub asks the stubbed Google (stubs/google) for ID tokens signed
// with the key it publishes as its JWKS, which the stubbed backend trusts
// through GOOGLE_CERTS_URL. Stubbed target only.
type GoogleStub struct {
	baseURL string
	http    *http.Client
}

// NewGoogleStub returns a client for the stub at baseURL.
func NewGoogleStub(baseURL string) *GoogleStub {
	return &GoogleStub{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 10 * time.Second}}
}

// IDToken mints an ID token for claims, as native Google Sign-In would
// return on the device.
func (g *GoogleStub) IDToken(ctx context.Context, claims idtoken.Claims) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+idtoken.MintPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("google stub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("google stub: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google stub: %s: %s", resp.Status, raw)
	}

	var minted idtoken.Minted
	if err := json.Unmarshal(raw, &minted); err != nil {
		return "", fmt.Errorf("google stub: decode: %w", err)
	}

	return minted.IDToken, nil
}
