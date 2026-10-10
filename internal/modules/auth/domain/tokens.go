// Package domain holds the authentication entities and rules: refresh-token
// families, password-reset records and the errors they produce.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Authentication errors. Credential failures deliberately share one message so
// responses never reveal whether an email address is registered.
var (
	ErrInvalidCredentials = apperror.New(http.StatusUnauthorized, "INVALID_CREDENTIALS", "The email address or password is incorrect.")
	ErrInvalidRefresh     = apperror.New(http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Your session has expired. Please sign in again.")
	ErrMissingRefresh     = apperror.New(http.StatusUnauthorized, "REFRESH_TOKEN_REQUIRED", "No session cookie was supplied.")
	ErrInvalidResetToken  = apperror.New(http.StatusBadRequest, "INVALID_RESET_TOKEN", "This password reset link is invalid or has expired.")
	ErrResetThrottled     = apperror.New(http.StatusTooManyRequests, "RESET_REQUEST_THROTTLED", "Too many password reset requests. Please try again later.")
	ErrSamePassword       = apperror.New(http.StatusUnprocessableEntity, "PASSWORD_UNCHANGED", "The new password must be different from the current password.")
	ErrAdminOnly          = apperror.New(http.StatusForbidden, "ADMIN_ACCESS_REQUIRED", "This account does not have administrative access.")
)

// RefreshToken is a server-side session record. Only the SHA-256 hash of the
// opaque token is stored, so a database leak cannot be replayed as a session.
//
// FamilyID groups every rotation of one login. If a token is presented twice,
// the whole family is revoked: either the token leaked or a client is broken,
// and both warrant ending the session.
type RefreshToken struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	UserID     bson.ObjectID `bson:"user_id"`
	TokenHash  string        `bson:"token_hash"`
	FamilyID   string        `bson:"family_id"`
	ExpiresAt  time.Time     `bson:"expires_at"`
	Revoked    bool          `bson:"revoked"`
	RevokedAt  *time.Time    `bson:"revoked_at,omitempty"`
	ReplacedBy string        `bson:"replaced_by,omitempty"`
	UserAgent  string        `bson:"user_agent,omitempty"`
	CreatedAt  time.Time     `bson:"created_at"`
}

// IsUsable reports whether the token may still be exchanged.
func (t *RefreshToken) IsUsable(now time.Time) bool {
	return !t.Revoked && now.Before(t.ExpiresAt)
}

// PasswordReset is a single-use, expiring authorization to set a new password.
type PasswordReset struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    bson.ObjectID `bson:"user_id"`
	TokenHash string        `bson:"token_hash"`
	ExpiresAt time.Time     `bson:"expires_at"`
	UsedAt    *time.Time    `bson:"used_at,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
}

// IsUsable reports whether the reset token may still be redeemed.
func (p *PasswordReset) IsUsable(now time.Time) bool {
	return p.UsedAt == nil && now.Before(p.ExpiresAt)
}

// RefreshTokenRepository persists refresh-token records.
type RefreshTokenRepository interface {
	// Create stores a new token record.
	Create(ctx context.Context, token *RefreshToken) error

	// FindByHash returns the record for a token hash, or ErrInvalidRefresh
	// when no record exists.
	FindByHash(ctx context.Context, hash string) (*RefreshToken, error)

	// Rotate atomically marks a token as consumed and records its successor.
	// It reports whether the token was still unconsumed, which is how reuse is
	// detected without a race.
	Rotate(ctx context.Context, hash, successorHash string) (bool, error)

	// RevokeFamily revokes every token in a family, ending the session.
	RevokeFamily(ctx context.Context, familyID string) error

	// RevokeAllForUser revokes every session for a user, used after a
	// password reset or change.
	RevokeAllForUser(ctx context.Context, userID bson.ObjectID) error
}

// PasswordResetRepository persists password-reset records.
type PasswordResetRepository interface {
	// Create stores a new reset record.
	Create(ctx context.Context, reset *PasswordReset) error

	// FindByHash returns the record for a token hash, or ErrInvalidResetToken.
	FindByHash(ctx context.Context, hash string) (*PasswordReset, error)

	// Consume atomically marks a reset as used, reporting whether this call
	// was the one that redeemed it.
	Consume(ctx context.Context, hash string) (bool, error)

	// CountSince counts a user's reset requests inside a window, to throttle
	// per account rather than only per IP.
	CountSince(ctx context.Context, userID bson.ObjectID, since time.Time) (int64, error)
}
