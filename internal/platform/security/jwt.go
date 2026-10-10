package security

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned for any token that fails signature, claim or
// expiry validation. Callers must not distinguish the reasons for clients.
var ErrInvalidToken = errors.New("security: invalid access token")

// Claims are the access-token claims. Authorization still re-checks the
// account server-side; the role claim only avoids a lookup on every request.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// TokenSigner issues and verifies EdDSA (Ed25519) access tokens.
//
// The key comes from JWT_ED25519_SEED when set. Otherwise it is derived
// deterministically from JWT_ACCESS_SECRET, which keeps local development to a
// single secret while allowing an explicit, rotatable key in production.
type TokenSigner struct {
	private  ed25519.PrivateKey
	public   ed25519.PublicKey
	issuer   string
	audience string
	ttl      time.Duration
}

// SignerOptions configures the token signer.
type SignerOptions struct {
	Secret   string
	Seed     string // base64-encoded 32-byte Ed25519 seed; takes precedence
	Issuer   string
	Audience string
	TTL      time.Duration
}

// NewTokenSigner builds the signer, deriving the key material once at startup.
func NewTokenSigner(opts SignerOptions) (*TokenSigner, error) {
	seed, err := resolveSeed(opts)
	if err != nil {
		return nil, err
	}

	private := ed25519.NewKeyFromSeed(seed)

	return &TokenSigner{
		private:  private,
		public:   private.Public().(ed25519.PublicKey),
		issuer:   opts.Issuer,
		audience: opts.Audience,
		ttl:      opts.TTL,
	}, nil
}

// resolveSeed returns the 32-byte Ed25519 seed from explicit configuration or,
// failing that, from the shared secret.
func resolveSeed(opts SignerOptions) ([]byte, error) {
	if opts.Seed != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.Seed)
		if err != nil {
			decoded, err = base64.RawURLEncoding.DecodeString(opts.Seed)
		}
		if err != nil {
			return nil, fmt.Errorf("security: JWT_ED25519_SEED must be base64-encoded: %w", err)
		}
		if len(decoded) != ed25519.SeedSize {
			return nil, fmt.Errorf("security: JWT_ED25519_SEED must decode to %d bytes, got %d", ed25519.SeedSize, len(decoded))
		}
		return decoded, nil
	}

	if opts.Secret == "" {
		return nil, fmt.Errorf("security: no signing key configured")
	}
	sum := sha256.Sum256([]byte(opts.Secret))
	return sum[:], nil
}

// TTL reports the access-token lifetime, which clients use to schedule
// pre-emptive refreshes.
func (s *TokenSigner) TTL() time.Duration { return s.ttl }

// Sign issues an access token for the user and returns it with its expiry.
func (s *TokenSigner) Sign(userID, role string) (token string, expiresAt time.Time, err error) {
	jti, err := NewID()
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now().UTC()
	expiresAt = now.Add(s.ttl)

	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        jti,
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(s.private)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("security: sign access token: %w", err)
	}

	return signed, expiresAt, nil
}

// Verify validates the signature, algorithm, issuer, audience and expiry, and
// returns the claims. Every failure maps to ErrInvalidToken.
func (s *TokenSigner) Verify(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		func(t *jwt.Token) (any, error) { return s.public, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid || claims.Subject == "" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
