// Package auth handles internal JWT issuance and verification.
//
// See: Security Architecture Section 2.2, ADR-007 (Identity Provider).
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("invalid or expired token")

// GarageClaim represents one entry in the "garages" JWT claim: the
// account's role at a specific garage.
type GarageClaim struct {
	GarageID string `json:"garage_id"`
	Role     string `json:"role"`
}

// Claims is the internal VietGara JWT payload.
// See: Security Architecture Section 2.2.
type Claims struct {
	jwt.RegisteredClaims
	Garages []GarageClaim `json:"garages"`
}

// HasAccessToGarage reports whether the token grants access to garageID,
// and if so, returns the role held there.
func (c *Claims) HasAccessToGarage(garageID string) (role string, ok bool) {
	for _, g := range c.Garages {
		if g.GarageID == garageID {
			return g.Role, true
		}
	}
	return "", false
}

type TokenIssuer struct {
	signingKey      []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewTokenIssuer(signingKey []byte, accessTTL, refreshTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{signingKey: signingKey, accessTokenTTL: accessTTL, refreshTokenTTL: refreshTTL}
}

// IssueAccessToken creates a short-lived (15-30 min) access token carrying
// the account_id and the list of garage_id+role the account can access.
// See: Security Architecture Section 2.2.
func (i *TokenIssuer) IssueAccessToken(accountID string, garages []GarageClaim) (string, time.Time, error) {
	expiresAt := time.Now().Add(i.accessTokenTTL)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
		Garages: garages,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.signingKey)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// NewRefreshToken generates an opaque, unguessable refresh token. Refresh
// tokens are NOT JWTs -- they are random strings stored server-side (Redis)
// so they can be revoked on logout. See Security Architecture Section 2.2.
func NewRefreshToken() string {
	return uuid.NewString() + "." + uuid.NewString()
}

// Verify parses and validates an access token, returning its claims.
func (i *TokenIssuer) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return i.signingKey, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
