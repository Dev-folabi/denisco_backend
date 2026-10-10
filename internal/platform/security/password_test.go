package security

import (
	"strings"
	"testing"
)

// testCost keeps bcrypt fast in tests while staying a valid cost.
const testCost = 10

func TestHasherRoundTrip(t *testing.T) {
	hasher := NewHasher(testCost)

	hash, err := hasher.Hash("correct horse battery")
	if err != nil {
		t.Fatalf("Hash() returned error: %v", err)
	}

	if !hasher.Verify(hash, "correct horse battery") {
		t.Error("Verify() rejected the correct password")
	}

	if hasher.Verify(hash, "wrong password") {
		t.Error("Verify() accepted an incorrect password")
	}
}

func TestHasherRejectsOversizedPassword(t *testing.T) {
	hasher := NewHasher(testCost)

	// bcrypt ignores input beyond 72 bytes; truncating silently would weaken
	// the password without telling anyone.
	if _, err := hasher.Hash(strings.Repeat("a", 73)); err == nil {
		t.Error("Hash() accepted a password longer than bcrypt's 72-byte limit")
	}
}

func TestHasherNeedsRehash(t *testing.T) {
	low := NewHasher(testCost)

	hash, err := low.Hash("some password")
	if err != nil {
		t.Fatalf("Hash() returned error: %v", err)
	}

	if low.NeedsRehash(hash) {
		t.Error("NeedsRehash() reported a rehash at the same cost")
	}

	high := NewHasher(testCost + 2)
	if !high.NeedsRehash(hash) {
		t.Error("NeedsRehash() did not detect a changed cost factor")
	}
}
