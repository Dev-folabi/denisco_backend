package security

import "testing"

func TestNewOpaqueTokenIsUniqueAndHashable(t *testing.T) {
	first, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken() returned error: %v", err)
	}

	second, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken() returned error: %v", err)
	}

	if first == second {
		t.Error("NewOpaqueToken() returned the same token twice")
	}

	if HashToken(first) == HashToken(second) {
		t.Error("HashToken() collided for distinct tokens")
	}

	if HashToken(first) != HashToken(first) {
		t.Error("HashToken() is not deterministic")
	}

	if len(HashToken(first)) != 64 {
		t.Errorf("HashToken() length = %d, want 64 hex characters", len(HashToken(first)))
	}
}

func TestSecureCompare(t *testing.T) {
	if !SecureCompare("abc", "abc") {
		t.Error("SecureCompare() reported equal strings as different")
	}
	if SecureCompare("abc", "abd") {
		t.Error("SecureCompare() reported different strings as equal")
	}
}
