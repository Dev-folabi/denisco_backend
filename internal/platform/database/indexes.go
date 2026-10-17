package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// indexSet pairs a collection with the indexes it needs.
type indexSet struct {
	collection string
	models     []mongo.IndexModel
}

// indexDefinitions mirrors the index strategy in DENISCO_Architecture.md §5.3.
// Index creation is idempotent, so running it on every boot is safe.
func indexDefinitions() []indexSet {
	return []indexSet{
		{
			collection: CollUsers,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "email", Value: 1}},
					Options: options.Index().SetName("uniq_email").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "role", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("role_created_at"),
				},
			},
		},
		{
			collection: CollRefreshTokens,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "token_hash", Value: 1}},
					Options: options.Index().SetName("uniq_token_hash").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "user_id", Value: 1}},
					Options: options.Index().SetName("user_id"),
				},
				{
					Keys:    bson.D{{Key: "family_id", Value: 1}},
					Options: options.Index().SetName("family_id"),
				},
				{
					// Cleanup only: expiry is still validated in application code.
					Keys:    bson.D{{Key: "expires_at", Value: 1}},
					Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0),
				},
			},
		},
		{
			collection: CollPasswordResets,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "token_hash", Value: 1}},
					Options: options.Index().SetName("uniq_token_hash").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("user_id_created_at"),
				},
				{
					Keys:    bson.D{{Key: "expires_at", Value: 1}},
					Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0),
				},
			},
		},
		{
			collection: CollProducts,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "slug", Value: 1}},
					Options: options.Index().SetName("uniq_slug").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "category_id", Value: 1}},
					Options: options.Index().SetName("status_category"),
				},
				{
					Keys:    bson.D{{Key: "name", Value: "text"}, {Key: "description", Value: "text"}},
					Options: options.Index().SetName("text_name_description"),
				},
			},
		},
		{
			collection: CollCategories,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "slug", Value: 1}},
					Options: options.Index().SetName("uniq_slug").SetUnique(true),
				},
			},
		},
		{
			collection: CollCarts,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "user_id", Value: 1}},
					Options: options.Index().SetName("uniq_user_id").SetUnique(true),
				},
			},
		},
		{
			collection: CollOrders,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "order_number", Value: 1}},
					Options: options.Index().SetName("uniq_order_number").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("user_id_created_at"),
				},
				{
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("status_created_at"),
				},
			},
		},
		{
			collection: CollPayments,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "reference", Value: 1}},
					Options: options.Index().SetName("uniq_reference").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "order_id", Value: 1}},
					Options: options.Index().SetName("order_id"),
				},
				{
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("status_created_at"),
				},
			},
		},
		{
			collection: CollInventory,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "product_id", Value: 1}},
					Options: options.Index().SetName("uniq_product_id").SetUnique(true),
				},
			},
		},
		{
			collection: CollInventoryMovements,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "product_id", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("product_id_created_at"),
				},
			},
		},
		{
			collection: CollDeliveryLocations,
			models: []mongo.IndexModel{
				{
					// The lowercased name makes "Wuse" and "wuse" collide, so
					// an area cannot be listed twice however it is capitalised.
					Keys:    bson.D{{Key: "name_key", Value: 1}},
					Options: options.Index().SetName("uniq_name_key").SetUnique(true),
				},
			},
		},
		{
			collection: CollSiteSettings,
			models: []mongo.IndexModel{
				{
					// The singleton document's identifier; unique by _id but
					// named so the intent is legible in the database.
					Keys:    bson.D{{Key: "_id", Value: 1}},
					Options: options.Index().SetName("uniq_key"),
				},
			},
		},
		{
			collection: CollConsultationTypes,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "position", Value: 1}},
					Options: options.Index().SetName("position"),
				},
				{
					// Sparse: only seeded types carry a prototype code.
					Keys:    bson.D{{Key: "code", Value: 1}},
					Options: options.Index().SetName("uniq_code").SetUnique(true).SetSparse(true),
				},
			},
		},
		{
			collection: CollConsultationSlots,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "date", Value: 1}, {Key: "time", Value: 1}},
					Options: options.Index().SetName("uniq_date_time").SetUnique(true),
				},
			},
		},
		{
			collection: CollConsultationBookings,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "reference", Value: 1}},
					Options: options.Index().SetName("uniq_reference").SetUnique(true),
				},
				{
					Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("user_id_created_at"),
				},
				{
					// One live booking per (date, time), whichever service
					// took it: the farm runs one consultation at a time, so a
					// booked hour is closed to every other service too. The
					// partial filter excludes cancelled bookings, so a
					// cancelled slot becomes bookable again while the record
					// is kept. This index, not a read-then-write check, is
					// what makes double-booking impossible under concurrency.
					Keys: bson.D{
						{Key: "date", Value: 1},
						{Key: "time", Value: 1},
					},
					Options: options.Index().
						SetName("uniq_active_slot_datetime").
						SetUnique(true).
						SetPartialFilterExpression(bson.M{"active": bson.M{"$eq": true}}),
				},
				{
					Keys:    bson.D{{Key: "type_id", Value: 1}, {Key: "date", Value: 1}},
					Options: options.Index().SetName("type_id_date"),
				},
				{
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "date", Value: 1}},
					Options: options.Index().SetName("status_date"),
				},
			},
		},
		{
			collection: CollOutboxEvents,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: 1}},
					Options: options.Index().SetName("status_created_at"),
				},
			},
		},
		{
			collection: CollAuditLogs,
			models: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "created_at", Value: -1}},
					Options: options.Index().SetName("created_at"),
				},
				{
					Keys:    bson.D{{Key: "actor_id", Value: 1}, {Key: "created_at", Value: -1}},
					Options: options.Index().SetName("actor_id_created_at"),
				},
			},
		},
	}
}

// supersededIndexes are indexes an earlier release created that a current
// definition replaces. MongoDB refuses to change the keys of an existing
// index, so the old one is dropped by name before the new set is applied.
//
// Dropping is safe in this direction only because each replacement is at
// least as strict as what it supersedes.
var supersededIndexes = []struct {
	collection string
	index      string
}{
	// Replaced by uniq_active_slot_datetime, which closes a booked time to
	// every consultation type rather than only to the one that took it.
	{collection: CollConsultationBookings, index: "uniq_active_slot"},
}

// EnsureIndexes applies every index definition. It is safe to call on each
// boot: MongoDB ignores an index that already exists with the same spec.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()

	for _, superseded := range supersededIndexes {
		if err := db.Collection(superseded.collection).Indexes().DropOne(ctx, superseded.index); err != nil {
			// IndexNotFound (27) is the normal case: a fresh database, or a
			// boot after the first one that dropped it.
			if !isIndexNotFound(err) {
				return fmt.Errorf("mongo: drop superseded index %s on %s: %w",
					superseded.index, superseded.collection, err)
			}
		}
	}

	// Checked before the unique index is created, so a database that predates
	// the one-session-per-slot rule fails with something an operator can act
	// on rather than a bare duplicate-key error from index creation.
	if err := checkSlotCollisions(ctx, db); err != nil {
		return err
	}

	for _, set := range indexDefinitions() {
		if _, err := db.Collection(set.collection).Indexes().CreateMany(ctx, set.models); err != nil {
			return fmt.Errorf("mongo: create indexes on %s: %w", set.collection, err)
		}
	}
	return nil
}

// checkSlotCollisions reports consultation times held by more than one live
// booking, which the global slot index would refuse to be created over.
//
// Such a pair can only exist in a database written before the rule changed,
// when two services could share an hour. Resolving it means cancelling one of
// the bookings, which is a decision for the farm, so this only reports.
func checkSlotCollisions(ctx context.Context, db *mongo.Database) error {
	cursor, err := db.Collection(CollConsultationBookings).Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"active": true}}},
		{{Key: "$group", Value: bson.M{
			"_id":        bson.M{"date": "$date", "time": "$time"},
			"count":      bson.M{"$sum": 1},
			"references": bson.M{"$push": "$reference"},
		}}},
		{{Key: "$match", Value: bson.M{"count": bson.M{"$gt": 1}}}},
		{{Key: "$limit", Value: 5}},
	})
	if err != nil {
		// A collection that does not exist yet has no collisions.
		if isNamespaceNotFound(err) {
			return nil
		}
		return fmt.Errorf("mongo: check consultation slot collisions: %w", err)
	}
	defer cursor.Close(ctx)

	var collisions []struct {
		ID struct {
			Date string `bson:"date"`
			Time string `bson:"time"`
		} `bson:"_id"`
		References []string `bson:"references"`
	}
	if err := cursor.All(ctx, &collisions); err != nil {
		return fmt.Errorf("mongo: decode consultation slot collisions: %w", err)
	}
	if len(collisions) == 0 {
		return nil
	}

	details := make([]string, 0, len(collisions))
	for _, collision := range collisions {
		details = append(details, fmt.Sprintf("%s %s (%s)",
			collision.ID.Date, collision.ID.Time, strings.Join(collision.References, ", ")))
	}

	return fmt.Errorf(
		"mongo: a consultation time is held by more than one live booking, which the "+
			"one-session-per-slot rule forbids: %s. Cancel all but one booking at each "+
			"of these times, then restart",
		strings.Join(details, "; "))
}

// isIndexNotFound reports whether an error is MongoDB's "no such index", or
// its "no such collection" — both mean there is nothing to drop.
func isIndexNotFound(err error) bool {
	return hasServerCode(err, indexNotFoundCode) || isNamespaceNotFound(err)
}

// isNamespaceNotFound reports whether an error is MongoDB's "no such
// collection".
func isNamespaceNotFound(err error) bool {
	return hasServerCode(err, namespaceNotFoundCode)
}

// hasServerCode reports whether an error carries a given MongoDB error code.
func hasServerCode(err error, code int) bool {
	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) {
		return serverErr.HasErrorCode(code)
	}
	return false
}

// MongoDB error codes for a missing index and a missing collection.
const (
	namespaceNotFoundCode = 26
	indexNotFoundCode     = 27
)
