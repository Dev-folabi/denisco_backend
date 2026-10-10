// Package infrastructure contains the MongoDB implementation of the user
// repository. Business rules stay in the domain and application layers.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists users in the `users` collection.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollUsers)}
}

// Create inserts the user, translating the unique-email violation into the
// domain error so callers never inspect driver errors.
func (r *MongoRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID.IsZero() {
		user.ID = bson.NewObjectID()
	}

	if _, err := r.collection.InsertOne(ctx, user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrEmailTaken
		}
		return fmt.Errorf("users: insert: %w", err)
	}

	return nil
}

// FindByID loads a user by identifier.
func (r *MongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// FindByEmail loads a user by normalized email address.
func (r *MongoRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, bson.M{"email": domain.NormalizeEmail(email)})
}

// findOne is the shared lookup, mapping "no documents" to the domain error.
func (r *MongoRepository) findOne(ctx context.Context, filter bson.M) (*domain.User, error) {
	var user domain.User
	if err := r.collection.FindOne(ctx, filter).Decode(&user); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("users: find: %w", err)
	}
	return &user, nil
}

// UpdatePasswordHash stores a new password hash.
func (r *MongoRepository) UpdatePasswordHash(ctx context.Context, id bson.ObjectID, hash string) error {
	result, err := r.collection.UpdateByID(ctx, id, bson.M{
		"$set": bson.M{"password_hash": hash, "updated_at": time.Now().UTC()},
	})
	if err != nil {
		return fmt.Errorf("users: update password hash: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// TouchLastLogin records the most recent successful authentication.
func (r *MongoRepository) TouchLastLogin(ctx context.Context, id bson.ObjectID, at time.Time) error {
	if _, err := r.collection.UpdateByID(ctx, id, bson.M{
		"$set": bson.M{"last_login_at": at, "updated_at": at},
	}); err != nil {
		return fmt.Errorf("users: touch last login: %w", err)
	}
	return nil
}

// List returns a page of accounts for the admin dashboard, newest first.
func (r *MongoRepository) List(ctx context.Context, filter domain.AdminFilter, page, limit int) ([]domain.User, int64, error) {
	query := bson.M{}

	// The customers screen lists customers; staff accounts are managed
	// elsewhere, so the role filter defaults to customers rather than all.
	if filter.Role != "" {
		query["role"] = filter.Role
	} else {
		query["role"] = domain.RoleCustomer
	}

	if filter.Search != "" {
		pattern := bson.M{"$regex": regexp.QuoteMeta(filter.Search), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"first_name": pattern},
			bson.M{"last_name": pattern},
			bson.M{"email": pattern},
			bson.M{"phone": pattern},
		}
	}

	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("users: count: %w", err)
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)).
			// The password hash never leaves the database for a listing.
			SetProjection(bson.M{"password_hash": 0}),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("users: list: %w", err)
	}
	defer cursor.Close(ctx)

	users := make([]domain.User, 0, limit)
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, fmt.Errorf("users: decode: %w", err)
	}

	return users, total, nil
}

// Count returns the number of accounts with a role, or all when empty.
func (r *MongoRepository) Count(ctx context.Context, role string) (int64, error) {
	query := bson.M{}
	if role != "" {
		query["role"] = role
	}

	count, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("users: count by role: %w", err)
	}
	return count, nil
}
