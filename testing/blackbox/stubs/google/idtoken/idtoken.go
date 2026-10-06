// Package idtoken is the stubbed Google's ID-token side: it publishes an
// RSA key as a JWKS, the way https://www.googleapis.com/oauth2/v3/certs
// does, and mints RS256 ID tokens signed with it on request. A backend
// whose GOOGLE_CERTS_URL points at CertsPath accepts those tokens as if
// Google had issued them.
package idtoken

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Paths served by Server.
const (
	// CertsPath is Google's JWKS path.
	CertsPath = "/oauth2/v3/certs"
	// MintPath is stub-only: POST Claims, receive Minted.
	MintPath = "/_stub/id-token"
)

// Issuer is the issuer Google puts in ID tokens.
const Issuer = "https://accounts.google.com"

const keyID = "blackbox-stub-key"

// Claims is the body of a MintPath request. Zero values get Google's
// defaults: Issuer, and an expiry one hour from now.
type Claims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name,omitempty"`
	Audience      string `json:"aud"`
	Issuer        string `json:"iss,omitempty"`
	// ExpiresIn is the token lifetime in seconds; negative mints an
	// already expired token.
	ExpiresIn int64 `json:"expires_in,omitempty"`
}

// Minted is the response to a MintPath request.
type Minted struct {
	IDToken string `json:"id_token"`
}

// Server serves CertsPath and MintPath.
type Server struct {
	key *rsa.PrivateKey
	mux *http.ServeMux
}

// NewServer generates a fresh signing key.
func NewServer() (*Server, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	s := &Server{key: key, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET "+CertsPath, s.certs)
	s.mux.HandleFunc("POST "+MintPath, s.mint)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) certs(w http.ResponseWriter, _ *http.Request) {
	pub := s.key.PublicKey
	encode := base64.RawURLEncoding.EncodeToString

	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kid": keyID, "kty": "RSA", "alg": "RS256", "use": "sig",
		"n": encode(pub.N.Bytes()), "e": encode(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (s *Server) mint(w http.ResponseWriter, r *http.Request) {
	var c Claims
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "invalid claims: "+err.Error(), http.StatusBadRequest)

		return
	}

	token, err := s.Sign(c, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, Minted{IDToken: token})
}

// Sign returns c as an RS256 JWT signed with the published key.
func (s *Server) Sign(c Claims, now time.Time) (string, error) {
	issuer := c.Issuer
	if issuer == "" {
		issuer = Issuer
	}

	lifetime := time.Duration(c.ExpiresIn) * time.Second
	if c.ExpiresIn == 0 {
		lifetime = time.Hour
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": issuer, "aud": c.Audience, "sub": c.Subject,
		"email": c.Email, "email_verified": c.EmailVerified, "name": c.Name,
		"iat": now.Unix(), "exp": now.Add(lifetime).Unix(),
	})
	token.Header["kid"] = keyID

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	return signed, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
