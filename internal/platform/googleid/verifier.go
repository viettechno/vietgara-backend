// Package googleid verifies Google ID tokens obtained natively by the mobile
// apps (Google Sign-In on iOS/Android): RS256 signature against Google's
// published keys, issuer, audience (the app's OAuth client IDs) and expiry.
//
// See: https://developers.google.com/identity/sign-in/ios/backend-auth
package googleid

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultCertsURL publishes Google's current ID-token signing keys (JWKS).
const DefaultCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

// The two issuer spellings Google uses.
var issuers = []string{"accounts.google.com", "https://accounts.google.com"}

// Errors returned by Verify.
var (
	// ErrNotConfigured: no mobile client ID is configured.
	ErrNotConfigured = errors.New("google id token sign-in is not configured")
	// ErrInvalidToken: the token is malformed, forged, expired, or for
	// another app.
	ErrInvalidToken = errors.New("google id token is invalid")
)

// Claims are the identity claims of a verified token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type tokenClaims struct {
	jwt.RegisteredClaims

	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // bool, or "true" in older tokens
	Name          string `json:"name"`
}

// Verifier checks ID tokens. Keys are fetched lazily, cached for as long as
// Google's Cache-Control allows, and refetched once on an unknown key id
// (Google rotates keys).
type Verifier struct {
	audiences []string
	certsURL  string
	client    *http.Client
	now       func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

// Option customizes a Verifier (tests).
type Option func(*Verifier)

// WithCertsURL fetches the keys from url instead of Google.
func WithCertsURL(url string) Option { return func(v *Verifier) { v.certsURL = url } }

// WithHTTPClient uses client to fetch the keys.
func WithHTTPClient(client *http.Client) Option { return func(v *Verifier) { v.client = client } }

// WithClock replaces time.Now.
func WithClock(now func() time.Time) Option { return func(v *Verifier) { v.now = now } }

// NewVerifier accepts tokens issued to any of audiences (the apps' OAuth
// client IDs: the Android "web/server" client and the iOS client).
func NewVerifier(audiences []string, opts ...Option) *Verifier {
	v := &Verifier{
		audiences: audiences,
		certsURL:  DefaultCertsURL,
		client:    &http.Client{Timeout: 10 * time.Second},
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(v)
	}

	return v
}

// Configured reports whether at least one audience is set.
func (v *Verifier) Configured() bool { return v != nil && len(v.audiences) > 0 }

// Verify checks token and returns its identity claims.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if !v.Configured() {
		return Claims{}, ErrNotConfigured
	}

	var claims tokenClaims

	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)

		return v.key(ctx, kid)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
		jwt.WithLeeway(time.Minute),
	)
	if err != nil || !parsed.Valid {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	if !slices.Contains(issuers, claims.Issuer) {
		return Claims{}, fmt.Errorf("%w: issuer %q", ErrInvalidToken, claims.Issuer)
	}

	if !slices.ContainsFunc(claims.Audience, func(aud string) bool { return slices.Contains(v.audiences, aud) }) {
		return Claims{}, fmt.Errorf("%w: audience %v", ErrInvalidToken, claims.Audience)
	}

	return Claims{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified == true || claims.EmailVerified == "true",
		Name:          claims.Name,
	}, nil
}

// key returns the public key with id kid, fetching the keys when they are
// missing, stale, or do not contain kid yet.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if key, ok := v.keys[kid]; ok && v.now().Before(v.expires) {
		return key, nil
	}

	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	key, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown signing key %q", kid)
	}

	return key, nil
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

var maxAgePattern = regexp.MustCompile(`max-age=(\d+)`)

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch google keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch google keys: %s", resp.Status)
	}

	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode google keys: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))

	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}

		key, err := rsaKey(k.N, k.E)
		if err != nil {
			return fmt.Errorf("google key %q: %w", k.Kid, err)
		}

		keys[k.Kid] = key
	}

	maxAge := time.Hour

	if m := maxAgePattern.FindStringSubmatch(resp.Header.Get("Cache-Control")); m != nil {
		if seconds, err := strconv.Atoi(m[1]); err == nil {
			maxAge = time.Duration(seconds) * time.Second
		}
	}

	v.keys = keys
	v.expires = v.now().Add(maxAge)

	return nil
}

func rsaKey(n, e string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(new(big.Int).SetBytes(eBytes).Int64())}, nil
}
