// Package infrastructure holds the MongoDB implementation of the category
// repository.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists categories in the `categories` collection.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollCategories)}
}

// List returns categories ordered by their display position.
func (r *MongoRepository) List(ctx context.Context, activeOnly bool) ([]domain.Category, error) {
	filter := bson.M{}
	if activeOnly {
		filter["active"] = true
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "position", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("categories: find: %w", err)
	}
	defer cursor.Close(ctx)

	categories := make([]domain.Category, 0, 8)
	if err := cursor.All(ctx, &categories); err != nil {
		return nil, fmt.Errorf("categories: decode: %w", err)
	}

	return categories, nil
}

// FindByID loads a category by identifier.
func (r *MongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Category, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// FindBySlug loads a category by slug.
func (r *MongoRepository) FindBySlug(ctx context.Context, slug string) (*domain.Category, error) {
	return r.findOne(ctx, bson.M{"slug": slug})
}

// findOne is the shared lookup, mapping "no documents" to the domain error.
func (r *MongoRepository) findOne(ctx context.Context, filter bson.M) (*domain.Category, error) {
	var category domain.Category
	if err := r.collection.FindOne(ctx, filter).Decode(&category); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, fmt.Errorf("categories: find: %w", err)
	}
	return &category, nil
}

// Upsert creates or refreshes a category by slug, leaving created_at intact on
// an existing record so re-running the seed is harmless.
func (r *MongoRepository) Upsert(ctx context.Context, category *domain.Category) error {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"slug": category.Slug},
		bson.M{
			"$set": bson.M{
				"name":        category.Name,
				"description": category.Description,
				"position":    category.Position,
				"active":      category.Active,
				"updated_at":  now,
			},
			"$setOnInsert": bson.M{"slug": category.Slug, "created_at": now},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("categories: upsert: %w", err)
	}

	if id, ok := result.UpsertedID.(bson.ObjectID); ok {
		category.ID = id
	}

	return nil
}
