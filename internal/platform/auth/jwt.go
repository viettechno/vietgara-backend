// Package auth handles internal JWT issuance and verification.
//
// The access token identifies the user only: garage access and
// permissions are read from the database on every request (Security
// Architecture Section 3.2), so a membership change or a garage created
// after sign-in takes effect immediately.
//
// See: Security Architecture Section 2.2, ADR-007 (Identity Provider).
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrInvalidToken is returned when a token is missing, malformed, or expired.
var ErrInvalidToken = errors.New("invalid or expired token")

// Claims is the internal VietGara JWT payload.
// See: Security Architecture Section 2.2.
type Claims struct {
	jwt.RegisteredClaims
	// EmailVerified gates every garage, staff and business API (FR-IAM-01).
	EmailVerified bool `json:"email_verified"`
	// PlatformAdmin grants the plan administration APIs (FR-LIC-05).
	PlatformAdmin bool `json:"platform_admin,omitempty"`
}

// UserID is the subject of the token.
func (c *Claims) UserID() string {
	return c.Subject
}

// Identity is what an access token asserts about its user.
type Identity struct {
	UserID        string
	EmailVerified bool
	PlatformAdmin bool
}

// TokenIssuer signs, issues, and verifies the VietGara JWTs.
type TokenIssuer struct {
	signingKey      []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	now             func() time.Time
}

// NewTokenIssuer returns a TokenIssuer for the given signing key and TTLs.
func NewTokenIssuer(signingKey []byte, accessTTL, refreshTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{signingKey: signingKey, accessTokenTTL: accessTTL, refreshTokenTTL: refreshTTL, now: time.Now}
}

// AccessTTL is the lifetime of access tokens.
func (i *TokenIssuer) AccessTTL() time.Duration { return i.accessTokenTTL }

// RefreshTTL is the lifetime of refresh tokens.
func (i *TokenIssuer) RefreshTTL() time.Duration { return i.refreshTokenTTL }

// IssueAccessToken creates a short-lived (15-30 min) access token for id.
func (i *TokenIssuer) IssueAccessToken(id Identity) (string, time.Time, error) {
	now := i.now()
	expiresAt := now.Add(i.accessTokenTTL)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
		EmailVerified: id.EmailVerified,
		PlatformAdmin: id.PlatformAdmin,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.signingKey)
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

// NewOpaqueToken generates an unguessable random token (256 bits,
// URL-safe). Refresh and password-reset tokens are opaque strings stored
// server-side (Redis) so they can be revoked.
func NewOpaqueToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf) // crypto/rand.Read never fails (Go 1.24+)

	return base64.RawURLEncoding.EncodeToString(buf)
}

// Verify parses and validates an access token, returning its claims. A
// leading "Bearer " is accepted.
func (i *TokenIssuer) Verify(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if len(tokenString) > len("bearer ") && strings.EqualFold(tokenString[:len("bearer ")], "bearer ") {
		tokenString = strings.TrimSpace(tokenString[len("bearer "):])
	}

	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}

		return i.signingKey, nil
	}, jwt.WithTimeFunc(i.now))
	if err != nil || !token.Valid || claims.Subject == "" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
