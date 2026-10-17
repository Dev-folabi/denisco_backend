// Package infrastructure holds the MongoDB implementation of the delivery
// repositories.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/delivery/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoLocationRepository persists delivery locations in the
// `delivery_locations` collection.
type MongoLocationRepository struct {
	collection *mongo.Collection
}

// NewMongoLocationRepository wires the repository to a database handle.
func NewMongoLocationRepository(db *mongo.Database) *MongoLocationRepository {
	return &MongoLocationRepository{collection: db.Collection(database.CollDeliveryLocations)}
}

// List returns locations ordered by name.
func (r *MongoLocationRepository) List(ctx context.Context, activeOnly bool) ([]domain.Location, error) {
	filter := bson.M{}
	if activeOnly {
		filter["active"] = true
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("delivery: find locations: %w", err)
	}
	defer cursor.Close(ctx)

	locations := make([]domain.Location, 0, 4)
	if err := cursor.All(ctx, &locations); err != nil {
		return nil, fmt.Errorf("delivery: decode locations: %w", err)
	}

	return locations, nil
}

// FindByID loads a location by identifier.
func (r *MongoLocationRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Location, error) {
	var location domain.Location
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&location); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrLocationNotFound
		}
		return nil, fmt.Errorf("delivery: find location: %w", err)
	}
	return &location, nil
}

// Create inserts a location.
func (r *MongoLocationRepository) Create(ctx context.Context, location *domain.Location) error {
	if location.ID.IsZero() {
		location.ID = bson.NewObjectID()
	}
	if _, err := r.collection.InsertOne(ctx, location); err != nil {
		return fmt.Errorf("delivery: insert location: %w", err)
	}
	return nil
}

// Update applies a set of changed fields.
func (r *MongoLocationRepository) Update(ctx context.Context, id bson.ObjectID, fields bson.M) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()

	result, err := r.collection.UpdateByID(ctx, id, bson.M{"$set": fields})
	if err != nil {
		return fmt.Errorf("delivery: update location: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrLocationNotFound
	}
	return nil
}

// Delete removes a location.
func (r *MongoLocationRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("delivery: delete location: %w", err)
	}
	if result.DeletedCount == 0 {
		return domain.ErrLocationNotFound
	}
	return nil
}

// MongoSettingsRepository persists the home-delivery master switch as a
// single document in the `site_settings` collection.
type MongoSettingsRepository struct {
	collection *mongo.Collection
}

// NewMongoSettingsRepository wires the repository to a database handle.
func NewMongoSettingsRepository(db *mongo.Database) *MongoSettingsRepository {
	return &MongoSettingsRepository{collection: db.Collection(database.CollSiteSettings)}
}

// settingsKey is the singleton document's identifier.
const settingsKey = "home_delivery"

// Get returns the stored settings, or the defaults when none has been
// written yet.
func (r *MongoSettingsRepository) Get(ctx context.Context) (domain.Settings, error) {
	var doc struct {
		Enabled *bool `bson:"enabled"`
	}
	err := r.collection.FindOne(ctx, bson.M{"_id": settingsKey}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return domain.Settings{Enabled: domain.DefaultEnabled}, nil
		}
		return domain.Settings{}, fmt.Errorf("delivery: read settings: %w", err)
	}

	settings := domain.Settings{Enabled: domain.DefaultEnabled}
	if doc.Enabled != nil {
		settings.Enabled = *doc.Enabled
	}
	return settings, nil
}

// SetEnabled stores the master switch.
func (r *MongoSettingsRepository) SetEnabled(ctx context.Context, enabled bool) error {
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": settingsKey},
		bson.M{"$set": bson.M{"enabled": enabled, "updated_at": now}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("delivery: write settings: %w", err)
	}
	return nil
}
