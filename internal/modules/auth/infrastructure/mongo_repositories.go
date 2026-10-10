// Package infrastructure contains the MongoDB implementations of the auth
// repositories.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/auth/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// RefreshTokenRepository persists refresh tokens in `refresh_tokens`.
type RefreshTokenRepository struct {
	collection *mongo.Collection
}

// NewRefreshTokenRepository wires the repository to a database handle.
func NewRefreshTokenRepository(db *mongo.Database) *RefreshTokenRepository {
	return &RefreshTokenRepository{collection: db.Collection(database.CollRefreshTokens)}
}

// Create stores a new refresh-token record.
func (r *RefreshTokenRepository) Create(ctx context.Context, token *domain.RefreshToken) error {
	if token.ID.IsZero() {
		token.ID = bson.NewObjectID()
	}
	if _, err := r.collection.InsertOne(ctx, token); err != nil {
		return fmt.Errorf("auth: insert refresh token: %w", err)
	}
	return nil
}

// FindByHash loads a record by token hash.
func (r *RefreshTokenRepository) FindByHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	var token domain.RefreshToken
	if err := r.collection.FindOne(ctx, bson.M{"token_hash": hash}).Decode(&token); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrInvalidRefresh
		}
		return nil, fmt.Errorf("auth: find refresh token: %w", err)
	}
	return &token, nil
}

// Rotate consumes a token in a single conditional update. The `revoked: false`
// filter is what makes concurrent refreshes safe: only the first caller
// matches, so a replayed token is detected rather than silently accepted.
func (r *RefreshTokenRepository) Rotate(ctx context.Context, hash, successorHash string) (bool, error) {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"token_hash": hash, "revoked": false},
		bson.M{"$set": bson.M{
			"revoked":     true,
			"revoked_at":  now,
			"replaced_by": successorHash,
		}},
	)
	if err != nil {
		return false, fmt.Errorf("auth: rotate refresh token: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// RevokeFamily revokes every token sharing a family identifier.
func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID string) error {
	return r.revoke(ctx, bson.M{"family_id": familyID, "revoked": false})
}

// RevokeAllForUser revokes every active session belonging to a user.
func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID bson.ObjectID) error {
	return r.revoke(ctx, bson.M{"user_id": userID, "revoked": false})
}

// revoke applies the revocation stamp to every matching record.
func (r *RefreshTokenRepository) revoke(ctx context.Context, filter bson.M) error {
	now := time.Now().UTC()
	if _, err := r.collection.UpdateMany(ctx, filter, bson.M{
		"$set": bson.M{"revoked": true, "revoked_at": now},
	}); err != nil {
		return fmt.Errorf("auth: revoke refresh tokens: %w", err)
	}
	return nil
}

// PasswordResetRepository persists reset records in `password_resets`.
type PasswordResetRepository struct {
	collection *mongo.Collection
}

// NewPasswordResetRepository wires the repository to a database handle.
func NewPasswordResetRepository(db *mongo.Database) *PasswordResetRepository {
	return &PasswordResetRepository{collection: db.Collection(database.CollPasswordResets)}
}

// Create stores a new reset record.
func (r *PasswordResetRepository) Create(ctx context.Context, reset *domain.PasswordReset) error {
	if reset.ID.IsZero() {
		reset.ID = bson.NewObjectID()
	}
	if _, err := r.collection.InsertOne(ctx, reset); err != nil {
		return fmt.Errorf("auth: insert password reset: %w", err)
	}
	return nil
}

// FindByHash loads a reset record by token hash.
func (r *PasswordResetRepository) FindByHash(ctx context.Context, hash string) (*domain.PasswordReset, error) {
	var reset domain.PasswordReset
	if err := r.collection.FindOne(ctx, bson.M{"token_hash": hash}).Decode(&reset); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrInvalidResetToken
		}
		return nil, fmt.Errorf("auth: find password reset: %w", err)
	}
	return &reset, nil
}

// Consume marks a reset token as used, but only if it is unused and unexpired.
// The conditional update makes the token genuinely single-use under
// concurrency.
func (r *PasswordResetRepository) Consume(ctx context.Context, hash string) (bool, error) {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{
			"token_hash": hash,
			"used_at":    nil,
			"expires_at": bson.M{"$gt": now},
		},
		bson.M{"$set": bson.M{"used_at": now}},
	)
	if err != nil {
		return false, fmt.Errorf("auth: consume password reset: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// CountSince counts a user's reset requests created within the window.
func (r *PasswordResetRepository) CountSince(ctx context.Context, userID bson.ObjectID, since time.Time) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"user_id":    userID,
		"created_at": bson.M{"$gte": since},
	})
	if err != nil {
		return 0, fmt.Errorf("auth: count password resets: %w", err)
	}
	return count, nil
}
