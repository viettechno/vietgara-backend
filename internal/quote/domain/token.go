package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// PublicTokenTTL is how long a LINK-mode approval link stays valid once
// sent (FR-QUO-02); SendQuote may be called again to issue a fresh one.
const PublicTokenTTL = 30 * 24 * time.Hour

// publicTokenBytes is the entropy of a generated token (32 bytes -> a
// 64-character hex string), comparable to a session token.
const publicTokenBytes = 32

// NewPublicToken returns a fresh random token; only its hash
// (HashPublicToken) is ever stored, so the plaintext exists solely in the
// SendQuote response and the e-mail sent to the customer.
func NewPublicToken() string {
	b := make([]byte, publicTokenBytes)
	_, _ = rand.Read(b) // crypto/rand never fails (Go 1.24+)

	return hex.EncodeToString(b)
}

// HashPublicToken hashes a plaintext token for storage and lookup. Unlike
// OTPs the token itself is high-entropy and globally unique, so the hash
// needs no additional binding.
func HashPublicToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
