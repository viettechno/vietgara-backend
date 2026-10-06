package idtoken_test

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// TestMintedTokensVerifyAgainstPublishedKeys checks the stub the way the
// backend's googleid verifier uses Google: fetch the JWKS, then verify an
// RS256 token by its kid.
func TestMintedTokensVerifyAgainstPublishedKeys(t *testing.T) {
	t.Parallel()

	stub, err := idtoken.NewServer()
	require.NoError(t, err)

	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)

	keys := fetchKeys(t, srv.URL+idtoken.CertsPath)

	resp, err := http.Post(srv.URL+idtoken.MintPath, "application/json", //nolint:noctx // test server
		strings.NewReader(`{"sub":"s1","email":"a@b.test","email_verified":true,"aud":"app"}`))
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	var minted idtoken.Minted
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&minted))

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(minted.IDToken, claims, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)

		return keys[kid], nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired())
	require.NoError(t, err)

	assert.Equal(t, idtoken.Issuer, claims["iss"])
	assert.Equal(t, "app", claims["aud"])
	assert.Equal(t, "a@b.test", claims["email"])
	assert.Equal(t, true, claims["email_verified"])
}

func TestSignOverrides(t *testing.T) {
	t.Parallel()

	stub, err := idtoken.NewServer()
	require.NoError(t, err)

	now := time.Unix(1_800_000_000, 0)

	token, err := stub.Sign(idtoken.Claims{Subject: "s", Audience: "app", Issuer: "https://evil.example.com", ExpiresIn: -60}, now)
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(token, claims)
	require.NoError(t, err)

	assert.Equal(t, "https://evil.example.com", claims["iss"])
	assert.InDelta(t, float64(now.Unix()-60), claims["exp"], 0)
}

func fetchKeys(t *testing.T, url string) map[string]*rsa.PublicKey {
	t.Helper()

	resp, err := http.Get(url) //nolint:noctx,gosec // the httptest server's URL
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	var set struct {
		Keys []struct{ Kid, N, E string } `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&set))
	require.Len(t, set.Keys, 1)

	keys := map[string]*rsa.PublicKey{}

	for _, k := range set.Keys {
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		require.NoError(t, err)
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		require.NoError(t, err)

		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}

	return keys
}
