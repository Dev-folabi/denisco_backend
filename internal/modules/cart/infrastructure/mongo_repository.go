// Package infrastructure holds the MongoDB implementation of the cart
// repository.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/cart/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists carts in the `carts` collection, one per customer.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollCarts)}
}

// Get returns the customer's cart, or an empty one when they have never added
// anything. Callers never have to handle "no cart yet" as a special case.
func (r *MongoRepository) Get(ctx context.Context, userID bson.ObjectID) (*domain.Cart, error) {
	var cart domain.Cart
	if err := r.collection.FindOne(ctx, bson.M{"user_id": userID}).Decode(&cart); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return &domain.Cart{UserID: userID, Items: []domain.Item{}}, nil
		}
		return nil, fmt.Errorf("cart: get: %w", err)
	}

	if cart.Items == nil {
		cart.Items = []domain.Item{}
	}

	return &cart, nil
}

// AddItem increases an existing line or appends a new one.
//
// Two updates are needed because MongoDB cannot both increment a matched array
// element and push a new one in a single statement. The increment is tried
// first; only when nothing matched is the line appended.
func (r *MongoRepository) AddItem(ctx context.Context, userID, productID bson.ObjectID, quantity int) error {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"user_id": userID, "items.product_id": productID},
		bson.M{
			"$inc": bson.M{"items.$.quantity": quantity},
			"$set": bson.M{"updated_at": now},
		},
	)
	if err != nil {
		return fmt.Errorf("cart: increase item: %w", err)
	}
	if result.MatchedCount == 1 {
		return r.capQuantity(ctx, userID, productID)
	}

	if _, err := r.collection.UpdateOne(ctx,
		bson.M{"user_id": userID},
		bson.M{
			"$push": bson.M{"items": domain.Item{ProductID: productID, Quantity: quantity, AddedAt: now}},
			"$set":  bson.M{"updated_at": now},
			"$setOnInsert": bson.M{
				"user_id":    userID,
				"created_at": now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("cart: add item: %w", err)
	}

	return nil
}

// capQuantity clamps a line to the maximum after an increment, so repeated
// "add to cart" clicks cannot push it past the bound.
func (r *MongoRepository) capQuantity(ctx context.Context, userID, productID bson.ObjectID) error {
	if _, err := r.collection.UpdateOne(ctx,
		bson.M{
			"user_id":          userID,
			"items.product_id": productID,
			"items.quantity":   bson.M{"$gt": domain.MaxLineQuantity},
		},
		bson.M{"$set": bson.M{"items.$.quantity": domain.MaxLineQuantity}},
	); err != nil {
		return fmt.Errorf("cart: cap quantity: %w", err)
	}
	return nil
}

// SetQuantity replaces a line's quantity.
func (r *MongoRepository) SetQuantity(ctx context.Context, userID, productID bson.ObjectID, quantity int) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"user_id": userID, "items.product_id": productID},
		bson.M{"$set": bson.M{
			"items.$.quantity": quantity,
			"updated_at":       time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, fmt.Errorf("cart: set quantity: %w", err)
	}

	return result.MatchedCount == 1, nil
}

// RemoveItem drops a line from the cart.
func (r *MongoRepository) RemoveItem(ctx context.Context, userID, productID bson.ObjectID) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"user_id": userID},
		bson.M{
			"$pull": bson.M{"items": bson.M{"product_id": productID}},
			"$set":  bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return false, fmt.Errorf("cart: remove item: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// Clear empties the cart without deleting the document, so the customer keeps
// one stable cart record.
func (r *MongoRepository) Clear(ctx context.Context, userID bson.ObjectID) error {
	if _, err := r.collection.UpdateOne(ctx,
		bson.M{"user_id": userID},
		bson.M{"$set": bson.M{"items": []domain.Item{}, "updated_at": time.Now().UTC()}},
	); err != nil {
		return fmt.Errorf("cart: clear: %w", err)
	}
	return nil
}
