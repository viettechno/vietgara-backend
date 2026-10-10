package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// TestGoogleIDTokenSignIn is the mobile apps' Google sign-in
// (POST /api/v1/auth/google/id-token) against the stubbed Google: the
// backend fetches the stub's JWKS through GOOGLE_CERTS_URL and accepts only
// tokens for its own client ID with a verified e-mail.
func TestGoogleIDTokenSignIn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	google := fixture.GoogleStub(t)
	api := fixture.Anonymous(t)
	audience := env.Get(t).GoogleClientID

	mint := func(t *testing.T, claims idtoken.Claims) string {
		t.Helper()

		token, err := google.IDToken(ctx, claims)
		require.NoError(t, err)

		return token
	}

	t.Run("a new Google user gets a verified user", func(t *testing.T) {
		email := fixture.UniqueEmail(t, "google")
		claims := idtoken.Claims{Subject: "google-" + email, Email: email, EmailVerified: true, Name: "Blackbox Google", Audience: audience}

		first, err := api.SignInWithGoogleIDToken(ctx, mint(t, claims))
		require.NoError(t, err)
		assert.Equal(t, email, first.GetUser().GetEmail())
		assert.True(t, first.GetUser().GetEmailVerified())
		assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE, first.GetUser().GetAuthProvider())

		again, err := api.SignInWithGoogleIDToken(ctx, mint(t, claims))
		require.NoError(t, err)
		assert.Equal(t, first.GetUser().GetId(), again.GetUser().GetId(), "the same Google subject is the same user")
	})

	t.Run("links Google to an existing e-mail user", func(t *testing.T) {
		owner := fixture.NewVerifiedOwner(t, "link")
		claims := idtoken.Claims{Subject: "google-" + owner.Email, Email: owner.Email, EmailVerified: true, Audience: audience}

		session, err := api.SignInWithGoogleIDToken(ctx, mint(t, claims))
		require.NoError(t, err)
		assert.Equal(t, owner.Session.GetUser().GetId(), session.GetUser().GetId())

		_, err = api.Login(ctx, owner.Email, owner.Password)
		require.NoError(t, err, "the password still works after linking")
	})

	t.Run("rejects tokens the backend must not trust", func(t *testing.T) {
		email := fixture.UniqueEmail(t, "google-bad")
		valid := idtoken.Claims{Subject: "google-" + email, Email: email, EmailVerified: true, Audience: audience}

		for name, tc := range map[string]struct {
			claims idtoken.Claims
			code   string
		}{
			"another app's client ID": {claims: with(valid, func(c *idtoken.Claims) { c.Audience = "someone-else.apps.googleusercontent.com" }), code: "GOOGLE_TOKEN_INVALID"},
			"another issuer":          {claims: with(valid, func(c *idtoken.Claims) { c.Issuer = "https://evil.example.com" }), code: "GOOGLE_TOKEN_INVALID"},
			"expired":                 {claims: with(valid, func(c *idtoken.Claims) { c.ExpiresIn = -3600 }), code: "GOOGLE_TOKEN_INVALID"},
			"unverified e-mail":       {claims: with(valid, func(c *idtoken.Claims) { c.EmailVerified = false }), code: "INVALID_CREDENTIALS"},
		} {
			_, err := api.SignInWithGoogleIDToken(ctx, mint(t, tc.claims))
			fixture.RequireAPIError(t, err, http.StatusUnauthorized, tc.code)
			t.Logf("%s: rejected", name)
		}

		_, err := api.SignInWithGoogleIDToken(ctx, "not-a-jwt")
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "GOOGLE_TOKEN_INVALID")
	})
}

func with(c idtoken.Claims, change func(*idtoken.Claims)) idtoken.Claims {
	change(&c)

	return c
}
