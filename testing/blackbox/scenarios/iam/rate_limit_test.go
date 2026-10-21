package iam_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// inFreshWindow waits for the start of the next minute when fewer than ten
// seconds of this one remain: the gateway counts per calendar minute, and a
// burst that straddles the boundary would be counted in two windows.
func inFreshWindow(t *testing.T) {
	t.Helper()

	now := time.Now()
	if remaining := time.Until(now.Truncate(time.Minute).Add(time.Minute)); remaining < 10*time.Second {
		select {
		case <-time.After(remaining + 200*time.Millisecond):
		case <-t.Context().Done():
		}
	}
}

// Scenario: E2E-IAM-028: a burst of 100 wrong sign-ins from one address is
// answered with 429 RATE_LIMITED once the gateway's strict limit is spent,
// while a legitimate user on another address signs in as usual. The limit is
// read from the gateway's own quota header, so the scenario follows the
// configuration. The stubbed gateway honours the client address header the
// suite sends; staging's does not (its limits are looser, see
// vietgara-infra).
func TestSignInBurstIsThrottled(t *testing.T) {
	t.Parallel()
	env.RequireStubbed(t, "pins the client address through a trusted forwarding header")

	ctx := t.Context()
	attacker := fixture.Anonymous(t).WithClientIP("203.0.113.27")
	victim := fixture.NewVerifiedOwner(t, "burst")

	inFreshWindow(t)

	var refused, throttled int

	for range 100 {
		_, err := attacker.Login(ctx, victim.Email, "Wrong#Password1")
		apiErr := client.AsAPIError(err)
		require.NotNil(t, apiErr, "a wrong password is an API error, got %v", err)

		switch apiErr.Status {
		case http.StatusUnauthorized:
			refused++
		case http.StatusTooManyRequests:
			assert.Equal(t, "RATE_LIMITED", apiErr.Code, "the gateway answers in the API's error envelope")

			throttled++
		default:
			require.Failf(t, "unexpected answer", "%v", apiErr)
		}
	}

	probe, err := attacker.Probe(ctx, http.MethodPost, "/api/v1/auth/login", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, probe.Status)

	limit, err := strconv.Atoi(probe.Header.Get("X-RateLimit-Limit-Minute"))
	require.NoError(t, err, "a throttled answer reports the quota")

	assert.LessOrEqual(t, refused, limit, "no more attempts than the limit are evaluated")
	assert.Equal(t, 100-refused, throttled)
	assert.NotEmpty(t, probe.Header.Get("Retry-After"))

	// The same address cannot guess the right password either: the throttle
	// sits in front of the credential check.
	_, err = attacker.Login(ctx, victim.Email, fixture.Password)
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "RATE_LIMITED")

	// Other addresses are unaffected.
	session, err := fixture.Anonymous(t).WithClientIP("203.0.113.28").Login(ctx, victim.Email, fixture.Password)
	require.NoError(t, err)
	assert.NotEmpty(t, session.GetAccessToken())
}

// unknownQuoteLink is well formed (32 bytes in hex) but belongs to no quote.
const unknownQuoteLink = "/api/v1/public/quotes/0000000000000000000000000000000000000000000000000000000000000000"

// Scenario: E2E-IAM-029: the customer's public quote link has its own strict
// limit per address: guessing tokens is throttled, a different address is not.
func TestPublicQuoteLinkIsThrottled(t *testing.T) {
	t.Parallel()
	env.RequireStubbed(t, "pins the client address through a trusted forwarding header")

	ctx := t.Context()
	guesser := fixture.Anonymous(t).WithClientIP("203.0.113.41")

	inFreshWindow(t)

	var throttled bool

	for range 100 {
		probe, err := guesser.Probe(ctx, http.MethodGet, unknownQuoteLink, nil)
		require.NoError(t, err)

		if probe.Status == http.StatusTooManyRequests {
			throttled = true

			break
		}

		assert.GreaterOrEqual(t, probe.Status, http.StatusBadRequest, "an unknown link is refused")
		assert.Less(t, probe.Status, http.StatusInternalServerError)
	}

	assert.True(t, throttled, "100 guesses in a row reach the limit")

	probe, err := fixture.Anonymous(t).WithClientIP("203.0.113.42").
		Probe(ctx, http.MethodGet, unknownQuoteLink, nil)
	require.NoError(t, err)
	assert.NotEqual(t, http.StatusTooManyRequests, probe.Status, "another address is not throttled")
}

// Scenario: E2E-IAM-030: every API answer carries the security headers and
// the remaining quota, and a page from another origin is not allowed to read
// it (no CORS grant).
func TestEdgeHeadersAndCORS(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	probe, err := fixture.Anonymous(t).WithHeader("Origin", "https://evil.example").
		Probe(ctx, http.MethodGet, "/api/v1/me", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, probe.Status)

	assert.Equal(t, "nosniff", probe.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", probe.Header.Get("X-Frame-Options"))
	assert.Contains(t, probe.Header.Get("Strict-Transport-Security"), "max-age=")
	assert.NotEmpty(t, probe.Header.Get("Referrer-Policy"))
	assert.Equal(t, "no-store", probe.Header.Get("Cache-Control"))
	assert.NotEmpty(t, probe.Header.Get("X-RateLimit-Limit-Minute"), "the default limit reports the quota")
	assert.Empty(t, probe.Header.Get("Access-Control-Allow-Origin"), "an unknown origin gets no CORS grant")

	// The sign-in route reports its own, stricter quota.
	login, err := fixture.Anonymous(t).Probe(ctx, http.MethodPost, "/api/v1/auth/login", []byte(`{}`))
	require.NoError(t, err)

	defaultLimit, err := strconv.Atoi(probe.Header.Get("X-RateLimit-Limit-Minute"))
	require.NoError(t, err)

	loginLimit, err := strconv.Atoi(login.Header.Get("X-RateLimit-Limit-Minute"))
	require.NoError(t, err)

	assert.Less(t, loginLimit, defaultLimit)
}
