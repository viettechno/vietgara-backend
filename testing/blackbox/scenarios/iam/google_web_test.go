package iam_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// Scenario: E2E-IAM-016 (partial): a Google sign-in cancelled in the popup
// ends with a "denied" result posted to the opener and creates no user;
// a callback without the state cookie is refused. The browser's
// pop-up blocker message is web-E2E.
func TestGoogleWebSignInCancelled(t *testing.T) {
	t.Parallel()
	env.RequireStubbed(t, "uses the stubbed Google authorization endpoint")

	ctx := t.Context()
	base := env.Get(t).APIURL

	noRedirect := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	login, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/auth/google/login", http.NoBody)
	require.NoError(t, err)

	resp, err := noRedirect.Do(login)
	require.NoError(t, err)

	_ = resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode, "the popup is redirected to Google")

	var state string

	for _, c := range resp.Cookies() {
		if c.Name != "" && c.Value != "" && strings.Contains(strings.ToLower(c.Name), "state") {
			state = c.Value
		}
	}

	require.NotEmpty(t, state, "a state cookie is set")

	location, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, state, location.Query().Get("state"))

	callback := func(rawQuery string, withCookie bool) string {
		t.Helper()

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/auth/google/callback?"+rawQuery, http.NoBody)
		require.NoError(t, reqErr)

		if withCookie {
			// The state cookie is Secure and the stubbed stack is plain HTTP,
			// so a cookie jar would not send it: set the header by hand.
			pairs := make([]string, 0, len(resp.Cookies()))
			for _, c := range resp.Cookies() {
				pairs = append(pairs, c.Name+"="+c.Value)
			}

			req.Header.Set("Cookie", strings.Join(pairs, "; "))
		}

		out, doErr := noRedirect.Do(req)
		require.NoError(t, doErr)

		defer func() { _ = out.Body.Close() }()

		body, readErr := io.ReadAll(out.Body)
		require.NoError(t, readErr)

		return string(body)
	}

	denied := callback("state="+url.QueryEscape(state)+"&error=access_denied", true)
	assert.Contains(t, denied, "GOOGLE_DENIED")
	assert.NotContains(t, denied, `"session"`, "no session is created")

	forged := callback("state="+url.QueryEscape(state)+"&code=any", false)
	assert.Contains(t, forged, "GOOGLE_STATE_INVALID")
}

// Scenario: E2E-IAM-017 (partial): asking a password reset for a Google-only
// user answers exactly like for any other address, so it reveals nothing.
// Whether a code is then sent is an open product question recorded in the
// scenario (September 28, 2026), so it is not asserted here.
func TestResetForGoogleOnlyUserRevealsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	google := fixture.GoogleStub(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "iam017")

	token, err := google.IDToken(ctx, idtoken.Claims{
		Subject: "google-" + email, Email: email, EmailVerified: true, Name: "Blackbox Google Only",
		Audience: env.Get(t).GoogleClientID,
	})
	require.NoError(t, err)

	session, err := api.SignInWithGoogleIDToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE, session.GetUser().GetAuthProvider())

	known, err := api.RequestPasswordReset(ctx, email)
	require.NoError(t, err)

	unknown, err := api.RequestPasswordReset(ctx, fixture.UniqueEmail(t, "iam017-nobody"))
	require.NoError(t, err)

	assert.Equal(t, unknown.GetResendAvailableAt() != nil, known.GetResendAvailableAt() != nil)
	assert.Equal(t, unknown.GetExpiresAt() != nil, known.GetExpiresAt() != nil)
}
