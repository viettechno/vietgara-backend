package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const testSigningKey = "test-signing-key"

func newTestIssuer(t *testing.T, accessTTL, refreshTTL time.Duration) *auth.TokenIssuer {
	t.Helper()
	return auth.NewTokenIssuer([]byte(testSigningKey), accessTTL, refreshTTL)
}

func signClaims(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()

	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)

	return signed
}

func TestClaims_HasAccessToGarage(t *testing.T) {
	claims := &auth.Claims{
		Garages: []auth.GarageClaim{
			{GarageID: "garage-1", Role: "owner"},
			{GarageID: "garage-2", Role: "staff"},
		},
	}

	tests := []struct {
		name     string
		garageID string
		wantRole string
		wantOK   bool
	}{
		{name: "garage with owner role", garageID: "garage-1", wantRole: "owner", wantOK: true},
		{name: "garage with staff role", garageID: "garage-2", wantRole: "staff", wantOK: true},
		{name: "unknown garage", garageID: "garage-99", wantOK: false},
		{name: "empty garage id", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			role, ok := claims.HasAccessToGarage(tt.garageID)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantRole, role)
		})
	}
}

func TestTokenIssuer_IssueAccessToken(t *testing.T) {
	garages := []auth.GarageClaim{
		{GarageID: "garage-1", Role: "owner"},
		{GarageID: "garage-2", Role: "staff"},
	}

	tests := []struct {
		name      string
		accessTTL time.Duration
	}{
		{name: "short lived token", accessTTL: 15 * time.Minute},
		{name: "long lived token", accessTTL: 12 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			issuer := newTestIssuer(t, tt.accessTTL, 24*time.Hour)

			token, expiresAt, err := issuer.IssueAccessToken("acc-1", garages)
			require.NoError(t, err)
			assert.NotEmpty(t, token)
			assert.WithinDuration(t, time.Now().Add(tt.accessTTL), expiresAt, time.Second)

			claims, err := issuer.Verify(token)
			require.NoError(t, err)
			assert.Equal(t, "acc-1", claims.Subject)
			assert.Equal(t, garages, claims.Garages)
			assert.WithinDuration(t, time.Now().Add(tt.accessTTL), claims.ExpiresAt.Time, time.Second)
			assert.NotEmpty(t, claims.ID)
		})
	}
}

func TestTokenIssuer_Verify(t *testing.T) {
	issuer := newTestIssuer(t, 15*time.Minute, 24*time.Hour)

	valid, _, err := issuer.IssueAccessToken("acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})
	require.NoError(t, err)

	expired := signClaims(t, jwt.SigningMethodHS256, []byte(testSigningKey), &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "acc-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	})

	wrongKey := signClaims(t, jwt.SigningMethodHS256, []byte("a-different-signing-key"), &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "acc-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	algNone := signClaims(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "acc-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{name: "valid token", token: valid},
		{name: "expired token", token: expired, wantErr: auth.ErrInvalidToken},
		{name: "token signed with wrong key", token: wrongKey, wantErr: auth.ErrInvalidToken},
		{name: "token using non-HMAC algorithm", token: algNone, wantErr: auth.ErrInvalidToken},
		{name: "malformed token", token: "not-a-jwt", wantErr: auth.ErrInvalidToken},
		{name: "empty token", token: "", wantErr: auth.ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := issuer.Verify(tt.token)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, claims)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, claims)
			assert.Equal(t, "acc-1", claims.Subject)
		})
	}
}

func TestNewRefreshToken(t *testing.T) {
	t.Run("has uuid.uuid format", func(t *testing.T) {
		token := auth.NewRefreshToken()

		parts := strings.Split(token, ".")

		require.Len(t, parts, 2)

		for _, part := range parts {
			_, err := uuid.Parse(part)
			require.NoError(t, err)
		}
	})

	t.Run("never collides across many calls", func(t *testing.T) {
		seen := make(map[string]struct{}, 1000)

		for range 1000 {
			token := auth.NewRefreshToken()
			if _, dup := seen[token]; dup {
				t.Fatalf("duplicate refresh token: %s", token)
			}

			seen[token] = struct{}{}
		}
	})
}
