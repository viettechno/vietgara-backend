package googleid_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/googleid"
)

const audience = "123-android.apps.googleusercontent.com"

var now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

type google struct {
	t       *testing.T
	keys    map[string]*rsa.PrivateKey
	fetches atomic.Int32
	server  *httptest.Server
}

func newGoogle(t *testing.T, kids ...string) *google {
	t.Helper()

	g := &google{t: t, keys: map[string]*rsa.PrivateKey{}}
	for _, kid := range kids {
		g.addKey(kid)
	}

	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.fetches.Add(1)

		type jwk struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		}

		set := struct {
			Keys []jwk `json:"keys"`
		}{}
		for kid, key := range g.keys {
			set.Keys = append(set.Keys, jwk{
				Kid: kid, Kty: "RSA",
				N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}

		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(g.server.Close)

	return g
}

func (g *google) addKey(kid string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(g.t, err)

	g.keys[kid] = key
}

func (g *google) verifier() *googleid.Verifier {
	return googleid.NewVerifier([]string{audience, "456-ios.apps.googleusercontent.com"},
		googleid.WithCertsURL(g.server.URL), googleid.WithHTTPClient(g.server.Client()),
		googleid.WithClock(func() time.Time { return now }))
}

func (g *google) sign(kid string, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(g.keys[kid])
	require.NoError(g.t, err)

	return signed
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": audience, "sub": "google-123",
		"email": "binh@example.com", "email_verified": true, "name": "Tran Thi Binh",
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

func TestVerify_AcceptsAValidToken(t *testing.T) {
	g := newGoogle(t, "k1")

	claims, err := g.verifier().Verify(context.Background(), g.sign("k1", validClaims()))

	require.NoError(t, err)
	assert.Equal(t, googleid.Claims{
		Subject: "google-123", Email: "binh@example.com", EmailVerified: true, Name: "Tran Thi Binh",
	}, claims)
}

func TestVerify_AcceptsTheOtherIssuerSpellingAndAStringEmailVerified(t *testing.T) {
	g := newGoogle(t, "k1")
	claims := validClaims()
	claims["iss"] = "accounts.google.com"
	claims["aud"] = "456-ios.apps.googleusercontent.com"
	claims["email_verified"] = "true"

	got, err := g.verifier().Verify(context.Background(), g.sign("k1", claims))

	require.NoError(t, err)
	assert.True(t, got.EmailVerified)
}

func TestVerify_CachesTheKeysAndRefetchesForANewKeyID(t *testing.T) {
	g := newGoogle(t, "k1")
	v := g.verifier()

	for range 2 {
		_, err := v.Verify(context.Background(), g.sign("k1", validClaims()))
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), g.fetches.Load())

	g.addKey("k2") // Google rotated its keys
	_, err := v.Verify(context.Background(), g.sign("k2", validClaims()))

	require.NoError(t, err)
	assert.Equal(t, int32(2), g.fetches.Load())
}

func TestVerify_RejectsInvalidTokens(t *testing.T) {
	g := newGoogle(t, "k1")
	other := newGoogle(t, "k1") // same key id, different key: a forgery

	cases := map[string]func() string{
		"another app's audience": func() string {
			c := validClaims()
			c["aud"] = "someone-else.apps.googleusercontent.com"

			return g.sign("k1", c)
		},
		"another issuer": func() string {
			c := validClaims()
			c["iss"] = "https://evil.example.com"

			return g.sign("k1", c)
		},
		"expired": func() string {
			c := validClaims()
			c["exp"] = now.Add(-time.Hour).Unix()

			return g.sign("k1", c)
		},
		"no expiry": func() string {
			c := validClaims()
			delete(c, "exp")

			return g.sign("k1", c)
		},
		"forged signature": func() string { return other.sign("k1", validClaims()) },
		"unknown key id": func() string {
			other.addKey("k9")

			return other.sign("k9", validClaims())
		},
		"not a jwt": func() string { return "not-a-token" },
		"HS256": func() string {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
			token.Header["kid"] = "k1"
			s, _ := token.SignedString([]byte("secret"))

			return s
		},
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := g.verifier().Verify(context.Background(), token())

			assert.ErrorIs(t, err, googleid.ErrInvalidToken)
		})
	}
}

func TestVerify_FailsWhenTheKeysCannotBeFetched(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	g := newGoogle(t, "k1")
	v := googleid.NewVerifier([]string{audience}, googleid.WithCertsURL(down.URL),
		googleid.WithClock(func() time.Time { return now }))

	_, err := v.Verify(context.Background(), g.sign("k1", validClaims()))

	assert.ErrorIs(t, err, googleid.ErrInvalidToken)
}

func TestVerify_RequiresAConfiguredAudience(t *testing.T) {
	v := googleid.NewVerifier(nil)

	assert.False(t, v.Configured())
	_, err := v.Verify(context.Background(), "anything")
	require.ErrorIs(t, err, googleid.ErrNotConfigured)
	assert.False(t, (*googleid.Verifier)(nil).Configured())
}
