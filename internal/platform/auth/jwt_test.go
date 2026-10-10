package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const testSigningKey = "test-signing-key"

func newTestIssuer(accessTTL time.Duration) *auth.TokenIssuer {
	return auth.NewTokenIssuer([]byte(testSigningKey), accessTTL, 30*24*time.Hour)
}

func TestTokenIssuer_IssueAndVerify(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(20 * time.Minute)
	assert.Equal(t, 20*time.Minute, issuer.AccessTTL())
	assert.Equal(t, 30*24*time.Hour, issuer.RefreshTTL())

	token, expiresAt, err := issuer.IssueAccessToken(auth.Identity{UserID: "user-1", EmailVerified: true, PlatformAdmin: true})
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(20*time.Minute), expiresAt, 5*time.Second)

	tests := []struct {
		name  string
		input string
	}{
		{name: "raw token", input: token},
		{name: "bearer prefix", input: "Bearer " + token},
		{name: "lower-case bearer and spaces", input: "  bearer   " + token + " "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			claims, err := issuer.Verify(tt.input)
			require.NoError(t, err)
			assert.Equal(t, "user-1", claims.UserID())
			assert.True(t, claims.EmailVerified)
			assert.True(t, claims.PlatformAdmin)
			assert.NotEmpty(t, claims.ID)
		})
	}
}

func TestTokenIssuer_VerifyRejects(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(time.Minute)
	expired := newTestIssuer(-time.Minute)

	expiredToken, _, err := expired.IssueAccessToken(auth.Identity{UserID: "user-1"})
	require.NoError(t, err)

	otherKey, _, err := auth.NewTokenIssuer([]byte("other"), time.Minute, time.Hour).IssueAccessToken(auth.Identity{UserID: "user-1"})
	require.NoError(t, err)

	noSubject, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
	}).SignedString([]byte(testSigningKey))
	require.NoError(t, err)

	noneAlg, err := jwt.NewWithClaims(jwt.SigningMethodNone, &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "garbage", token: "not-a-jwt"},
		{name: "expired", token: expiredToken},
		{name: "wrong signing key", token: otherKey},
		{name: "missing subject", token: noSubject},
		{name: "alg none", token: noneAlg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := issuer.Verify(tt.token)
			require.ErrorIs(t, err, auth.ErrInvalidToken)
		})
	}
}

func TestNewOpaqueToken(t *testing.T) {
	t.Parallel()

	a, b := auth.NewOpaqueToken(), auth.NewOpaqueToken()
	assert.Len(t, a, 43)
	assert.NotEqual(t, a, b)
}
