// Package infrastructure holds the MongoDB implementation of the inventory
// repository. Every mutation is a single conditional update, which is what
// makes concurrent stock changes safe without locking.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists stock in `inventory` and its history in
// `inventory_movements`.
type MongoRepository struct {
	records   *mongo.Collection
	movements *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{
		records:   db.Collection(database.CollInventory),
		movements: db.Collection(database.CollInventoryMovements),
	}
}

// EnsureRecord creates a zeroed record the first time a product is stocked.
func (r *MongoRepository) EnsureRecord(ctx context.Context, productID bson.ObjectID) error {
	now := time.Now().UTC()

	_, err := r.records.UpdateOne(ctx,
		bson.M{"product_id": productID},
		bson.M{"$setOnInsert": bson.M{
			"product_id":    productID,
			"available_qty": 0,
			"reserved_qty":  0,
			"updated_at":    now,
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("inventory: ensure record: %w", err)
	}

	return nil
}

// Get loads one product's stock record.
func (r *MongoRepository) Get(ctx context.Context, productID bson.ObjectID) (*domain.Inventory, error) {
	var record domain.Inventory
	if err := r.records.FindOne(ctx, bson.M{"product_id": productID}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrInventoryNotFound
		}
		return nil, fmt.Errorf("inventory: get: %w", err)
	}
	return &record, nil
}

// ListByProducts loads stock for a set of products in one query.
func (r *MongoRepository) ListByProducts(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]domain.Inventory, error) {
	stock := make(map[bson.ObjectID]domain.Inventory, len(productIDs))
	if len(productIDs) == 0 {
		return stock, nil
	}

	cursor, err := r.records.Find(ctx, bson.M{"product_id": bson.M{"$in": productIDs}})
	if err != nil {
		return nil, fmt.Errorf("inventory: list by products: %w", err)
	}
	defer cursor.Close(ctx)

	var records []domain.Inventory
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("inventory: decode: %w", err)
	}

	for _, record := range records {
		stock[record.ProductID] = record
	}

	return stock, nil
}

// List returns a page of stock records for the admin overview.
func (r *MongoRepository) List(ctx context.Context, page, limit int) ([]domain.Inventory, int64, error) {
	total, err := r.records.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("inventory: count: %w", err)
	}

	cursor, err := r.records.Find(ctx, bson.M{},
		options.Find().
			SetSort(bson.D{{Key: "updated_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("inventory: list: %w", err)
	}
	defer cursor.Close(ctx)

	records := make([]domain.Inventory, 0, limit)
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("inventory: decode: %w", err)
	}

	return records, total, nil
}

// Reserve promises stock to an order.
//
// The $expr filter is the whole safety property: the update applies only while
// available - reserved is still at least the requested quantity, so two
// customers racing for the last unit cannot both succeed.
func (r *MongoRepository) Reserve(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{
			"product_id": productID,
			"$expr": bson.M{"$gte": bson.A{
				bson.M{"$subtract": bson.A{"$available_qty", "$reserved_qty"}},
				quantity,
			}},
		},
		bson.M{"$inc": bson.M{"reserved_qty": quantity}},
	)
}

// Release returns reserved stock to the sellable pool when an order is
// cancelled or its reservation expires.
func (r *MongoRepository) Release(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{"product_id": productID, "reserved_qty": bson.M{"$gte": quantity}},
		bson.M{"$inc": bson.M{"reserved_qty": -quantity}},
	)
}

// Deduct removes stock that has been paid for, clearing it from both the
// on-hand and reserved quantities in one step.
func (r *MongoRepository) Deduct(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{
			"product_id":    productID,
			"reserved_qty":  bson.M{"$gte": quantity},
			"available_qty": bson.M{"$gte": quantity},
		},
		bson.M{"$inc": bson.M{"reserved_qty": -quantity, "available_qty": -quantity}},
	)
}

// SellWithoutReservation takes stock for an order that holds no reservation,
// which is how a late payment is honoured when the units are still on the
// shelf. The sellable condition is the same one Reserve uses, so a late
// payment can never take stock that another customer has already been
// promised.
func (r *MongoRepository) SellWithoutReservation(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{
			"product_id": productID,
			"$expr": bson.M{"$gte": bson.A{
				bson.M{"$subtract": bson.A{"$available_qty", "$reserved_qty"}},
				quantity,
			}},
		},
		bson.M{"$inc": bson.M{"available_qty": -quantity}},
	)
}

// Adjust applies an admin correction, refusing to drop on-hand stock below
// what is already reserved for unpaid orders.
func (r *MongoRepository) Adjust(ctx context.Context, productID bson.ObjectID, delta int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{
			"product_id": productID,
			"$expr": bson.M{"$gte": bson.A{
				bson.M{"$add": bson.A{"$available_qty", delta}},
				"$reserved_qty",
			}},
		},
		bson.M{"$inc": bson.M{"available_qty": delta}},
	)
}

// SetAvailable replaces the on-hand quantity, used when an admin edits a
// product's stock directly. Reserved stock still sets the floor.
func (r *MongoRepository) SetAvailable(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error) {
	return r.conditionalUpdate(ctx,
		bson.M{"product_id": productID, "reserved_qty": bson.M{"$lte": quantity}},
		bson.M{"$set": bson.M{"available_qty": quantity}},
	)
}

// conditionalUpdate applies the change and reports whether the precondition in
// the filter held.
func (r *MongoRepository) conditionalUpdate(ctx context.Context, filter, update bson.M) (bool, error) {
	if set, ok := update["$set"].(bson.M); ok {
		set["updated_at"] = time.Now().UTC()
	} else {
		update["$set"] = bson.M{"updated_at": time.Now().UTC()}
	}

	result, err := r.records.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("inventory: update: %w", err)
	}

	return result.MatchedCount == 1, nil
}

// RecordMovement appends an entry to the stock history.
func (r *MongoRepository) RecordMovement(ctx context.Context, movement *domain.Movement) error {
	if movement.ID.IsZero() {
		movement.ID = bson.NewObjectID()
	}
	if movement.CreatedAt.IsZero() {
		movement.CreatedAt = time.Now().UTC()
	}

	if _, err := r.movements.InsertOne(ctx, movement); err != nil {
		return fmt.Errorf("inventory: record movement: %w", err)
	}

	return nil
}

// PurgeProduct removes a product's stock record and movement log.
//
// Both go together: a movement log with no product and no stock record would
// be unreadable, and the catalogue only deletes a product that no order ever
// referenced, so there is no history worth keeping.
func (r *MongoRepository) PurgeProduct(ctx context.Context, productID bson.ObjectID) error {
	if _, err := r.records.DeleteOne(ctx, bson.M{"product_id": productID}); err != nil {
		return fmt.Errorf("inventory: delete stock record: %w", err)
	}
	if _, err := r.movements.DeleteMany(ctx, bson.M{"product_id": productID}); err != nil {
		return fmt.Errorf("inventory: delete movements: %w", err)
	}
	return nil
}

// Movements returns a page of stock history, newest first.
func (r *MongoRepository) Movements(ctx context.Context, productID *bson.ObjectID, page, limit int) ([]domain.Movement, int64, error) {
	filter := bson.M{}
	if productID != nil {
		filter["product_id"] = *productID
	}

	total, err := r.movements.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("inventory: count movements: %w", err)
	}

	cursor, err := r.movements.Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("inventory: list movements: %w", err)
	}
	defer cursor.Close(ctx)

	movements := make([]domain.Movement, 0, limit)
	if err := cursor.All(ctx, &movements); err != nil {
		return nil, 0, fmt.Errorf("inventory: decode movements: %w", err)
	}

	return movements, total, nil
}
