// Package security holds the cryptographic primitives shared by the modules:
// password hashing, access-token signing and opaque-token generation.
package security

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Hasher hashes and verifies passwords with bcrypt at a configured cost.
// The cost should be benchmarked on the production VPS.
type Hasher struct {
	cost int
}

// NewHasher returns a hasher at the given cost, falling back to bcrypt's
// default when the value is out of range.
func NewHasher(cost int) *Hasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}
	return &Hasher{cost: cost}
}

// Cost reports the configured bcrypt cost.
func (h *Hasher) Cost() int { return h.cost }

// Hash derives a bcrypt hash. Passwords longer than bcrypt's 72-byte input
// limit are rejected rather than silently truncated.
func (h *Hasher) Hash(password string) (string, error) {
	if len(password) > 72 {
		return "", fmt.Errorf("security: password exceeds bcrypt's 72-byte limit")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("security: hash password: %w", err)
	}
	return string(hash), nil
}

// Verify reports whether the password matches the stored hash.
func (h *Hasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NeedsRehash reports whether a stored hash was produced at a different cost
// than the one now configured, so it can be upgraded on a successful login.
func (h *Hasher) NeedsRehash(hash string) bool {
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return false
	}
	return cost != h.cost
}
