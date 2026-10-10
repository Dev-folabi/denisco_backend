package security

import (
	"testing"
	"time"
)

// newTestSigner builds a signer with deterministic key material.
func newTestSigner(t *testing.T, ttl time.Duration) *TokenSigner {
	t.Helper()

	signer, err := NewTokenSigner(SignerOptions{
		Secret:   "a-test-secret-that-is-long-enough-to-pass",
		Issuer:   "denisco_backend",
		Audience: "denisco_api",
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}
	return signer
}

func TestSignAndVerify(t *testing.T) {
	signer := newTestSigner(t, 15*time.Minute)

	token, expiresAt, err := signer.Sign("65f0c0ffee0000000000beef", "customer")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	if !expiresAt.After(time.Now().UTC()) {
		t.Error("Sign() returned an expiry in the past")
	}

	claims, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("Verify() returned error: %v", err)
	}

	if claims.Subject != "65f0c0ffee0000000000beef" {
		t.Errorf("subject = %q, want the signed user id", claims.Subject)
	}
	if claims.Role != "customer" {
		t.Errorf("role = %q, want \"customer\"", claims.Role)
	}
	if claims.ID == "" {
		t.Error("claims are missing a jti")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	signer := newTestSigner(t, -time.Minute)

	token, _, err := signer.Sign("65f0c0ffee0000000000beef", "customer")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	if _, err := signer.Verify(token); err == nil {
		t.Error("Verify() accepted an expired token")
	}
}

func TestVerifyRejectsForeignSigner(t *testing.T) {
	issuer := newTestSigner(t, 15*time.Minute)

	other, err := NewTokenSigner(SignerOptions{
		Secret:   "a-different-secret-that-is-also-long-enough",
		Issuer:   "denisco_backend",
		Audience: "denisco_api",
		TTL:      15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}

	token, _, err := issuer.Sign("65f0c0ffee0000000000beef", "admin")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	if _, err := other.Verify(token); err == nil {
		t.Error("Verify() accepted a token signed with another key")
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	signer := newTestSigner(t, 15*time.Minute)

	token, _, err := signer.Sign("65f0c0ffee0000000000beef", "customer")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	stricter, err := NewTokenSigner(SignerOptions{
		Secret:   "a-test-secret-that-is-long-enough-to-pass",
		Issuer:   "denisco_backend",
		Audience: "another_api",
		TTL:      15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}

	if _, err := stricter.Verify(token); err == nil {
		t.Error("Verify() accepted a token issued for a different audience")
	}
}

func TestSeedTakesPrecedenceOverSecret(t *testing.T) {
	// A 32-byte seed, base64-encoded.
	const seed = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

	first, err := NewTokenSigner(SignerOptions{Secret: "secret-one-long-enough-for-config", Seed: seed, Issuer: "i", Audience: "a", TTL: time.Minute})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}

	second, err := NewTokenSigner(SignerOptions{Secret: "secret-two-also-long-enough-here!", Seed: seed, Issuer: "i", Audience: "a", TTL: time.Minute})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}

	token, _, err := first.Sign("65f0c0ffee0000000000beef", "admin")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	// Both signers share the explicit seed, so the second must accept the
	// first's token despite the different secrets.
	if _, err := second.Verify(token); err != nil {
		t.Errorf("Verify() rejected a token signed with the same seed: %v", err)
	}
}

func TestSeedMustBeCorrectLength(t *testing.T) {
	if _, err := NewTokenSigner(SignerOptions{Seed: "c2hvcnQ=", Issuer: "i", Audience: "a", TTL: time.Minute}); err == nil {
		t.Error("NewTokenSigner() accepted a seed of the wrong length")
	}
}
