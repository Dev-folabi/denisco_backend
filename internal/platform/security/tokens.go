package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// opaqueTokenBytes is the entropy behind refresh and password-reset tokens.
const opaqueTokenBytes = 32

// NewOpaqueToken returns a cryptographically random, URL-safe token. Only its
// hash is ever persisted, so the plaintext exists just long enough to reach
// the client.
func NewOpaqueToken() (string, error) {
	buf := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("security: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the SHA-256 hex digest used as the stored lookup key.
// SHA-256 is correct here (not bcrypt): the input is already high-entropy and
// lookups must be exact and fast.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewID returns a random identifier for token families and JWT IDs.
func NewID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("security: generate id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SecureCompare compares two strings in constant time.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
