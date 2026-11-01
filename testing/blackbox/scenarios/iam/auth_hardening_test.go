package iam_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// authRoutes are every route under /api/v1/auth that an anonymous caller
// can reach, each with a body the API reads and refuses. They share the
// gateway's strict per-address limit.
var authRoutes = []struct {
	name, path, body string
}{
	{"sign up", "/api/v1/auth/register", `{"email":"nobody@blackbox.vietgara.test","password":"x","fullName":"x"}`},
	{"sign in", "/api/v1/auth/login", `{"email":"nobody@blackbox.vietgara.test","password":"x"}`},
	{"resend the verification code", "/api/v1/auth/email-verification/otp", `{"email":"nobody@blackbox.vietgara.test"}`},
	{"verify the e-mail", "/api/v1/auth/email-verification", `{"email":"nobody@blackbox.vietgara.test","otp":"000000"}`},
	{"ask for a password reset code", "/api/v1/auth/password-reset/otp", `{"email":"nobody@blackbox.vietgara.test"}`},
	{"verify the reset code", "/api/v1/auth/password-reset/otp/verification", `{"email":"nobody@blackbox.vietgara.test","otp":"000000"}`},
	{"set the new password", "/api/v1/auth/password-reset", `{"resetToken":"nope","newPassword":"Blackbox#2030"}`},
	{"second step of a sign-in", "/api/v1/auth/mfa/challenge", `{"challengeToken":"nope","code":"000000"}`},
	{"Google ID token", "/api/v1/auth/google/id-token", `{"idToken":"nope"}`},
}

// Scenario: E2E-SYS-006: every anonymous route under /auth shares the
// strict per-address limit: a burst from one address is answered 429
// RATE_LIMITED with the quota headers, and another address is unaffected.
func TestEveryAuthRouteIsThrottled(t *testing.T) {
	t.Parallel()
	env.RequireStubbed(t, "pins the client address through a trusted forwarding header")

	inFreshWindow(t)

	for index, route := range authRoutes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			burst := fixture.Anonymous(t).WithClientIP(fmt.Sprintf("203.0.113.%d", 100+index))

			var throttled int

			for range 60 {
				probe, err := burst.Probe(ctx, http.MethodPost, route.path, []byte(route.body))
				require.NoError(t, err)

				if probe.Status == http.StatusTooManyRequests {
					throttled++

					assert.Contains(t, string(probe.Body), "RATE_LIMITED")
					assert.NotEmpty(t, probe.Header.Get("Retry-After"))
				}
			}

			assert.Positive(t, throttled, "60 requests in a minute pass the strict limit")

			calm := fixture.Anonymous(t).WithClientIP(fmt.Sprintf("203.0.113.%d", 150+index))
			probe, err := calm.Probe(ctx, http.MethodPost, route.path, []byte(route.body))
			require.NoError(t, err)
			assert.NotEqual(t, http.StatusTooManyRequests, probe.Status, "another address is not throttled")
		})
	}
}

// Scenario: E2E-SYS-007: a refresh token works once: refreshing rotates it,
// the old one is refused, the new one works, and two refreshes at the same
// moment give one new session.
func TestRefreshTokensRotate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "sys-refresh")
	api := fixture.Anonymous(t)

	session, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	rotated, err := api.Refresh(ctx, session.GetRefreshToken())
	require.NoError(t, err)
	assert.NotEqual(t, session.GetRefreshToken(), rotated.GetRefreshToken(), "a new refresh token every time")

	_, err = api.Refresh(ctx, session.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID")

	again, err := api.Refresh(ctx, rotated.GetRefreshToken())
	require.NoError(t, err, "the newest token works")

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)

	for range 6 {
		wg.Go(func() {
			if _, refreshErr := api.Refresh(ctx, again.GetRefreshToken()); refreshErr == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	assert.Equal(t, 1, accepted, "one of six simultaneous refreshes wins")
}

// Scenario: E2E-SYS-008: the password rules at their edges, and e-mail
// addresses that differ only in case or spacing are one user.
func TestCredentialRulesAtTheirEdges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	api := fixture.Anonymous(t)

	register := func(email, password string) error {
		_, err := api.Register(ctx, &identityv1.RegisterRequest{Email: email, Password: password, FullName: "QA Edge"})

		return err
	}

	t.Run("passwords are 8 characters to 72 bytes, with no composition rule", func(t *testing.T) {
		// bcrypt reads 72 bytes only: a longer password must be refused,
		// not silently truncated, and the message says so.
		for _, password := range []string{"", "Ab1#xyz"} {
			err := register(fixture.UniqueEmail(t, "pw"), password)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
			assert.Contains(t, client.AsAPIError(err).Message, "at least 8 characters")
		}

		for _, password := range []string{strings.Repeat("Ab1#", 18) + "x", strings.Repeat("Ab1#", 50)} {
			err := register(fixture.UniqueEmail(t, "pw"), password)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
			assert.Contains(t, client.AsAPIError(err).Message, "at most 72 bytes")
		}

		for name, password := range map[string]string{
			"eight letters":      "abcdefgh",
			"eight digits":       "12345678",
			"exactly 72 bytes":   strings.Repeat("Ab1#", 18),
			"spaces":             strings.Repeat(" ", 20),
			"Vietnamese letters": "Mật khẩu Việt 123",
		} {
			require.NoError(t, register(fixture.UniqueEmail(t, "pw-ok"), password), name)
		}
	})

	t.Run("e-mail case and spacing", func(t *testing.T) {
		email := fixture.UniqueEmail(t, "case")
		require.NoError(t, register(email, fixture.Password))

		err := register(strings.ToUpper(email), fixture.Password)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		err = register("  "+email+"  ", fixture.Password)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})
}
