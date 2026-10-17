// Package secretbox encrypts small secrets at rest (the TOTP secrets of
// MFA) with AES-256-GCM. The key comes from configuration, never from the
// database that holds the ciphertext.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// Errors of the package.
var (
	ErrInvalidKey = errors.New("secretbox: the key must be 64 hex characters (32 bytes)")
	ErrCorrupt    = errors.New("secretbox: ciphertext is too short or does not authenticate")
)

// Box seals and opens secrets.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for a hex-encoded 32-byte key (`openssl rand -hex 32`).
func New(hexKey string) (*Box, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != KeySize {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: new cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: new gcm: %w", err)
	}

	return &Box{aead: aead}, nil
}

// Seal returns nonce || ciphertext of plaintext. The nonce is random, so
// sealing the same value twice gives different results.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretbox: nonce: %w", err)
	}

	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal; it fails when the data was altered or sealed with
// another key.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	size := b.aead.NonceSize()
	if len(sealed) < size+b.aead.Overhead() {
		return nil, ErrCorrupt
	}

	plaintext, err := b.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}

	return plaintext, nil
}
