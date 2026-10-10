// Package infrastructure holds the MongoDB implementation of the payment
// repository.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists payment attempts in the `payments` collection.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollPayments)}
}

// Create inserts a payment attempt. The unique reference index is the final
// guard against two attempts sharing one reference.
func (r *MongoRepository) Create(ctx context.Context, payment *domain.Payment) error {
	if payment.ID.IsZero() {
		payment.ID = bson.NewObjectID()
	}

	if _, err := r.collection.InsertOne(ctx, payment); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("payments: duplicate reference: %w", err)
		}
		return fmt.Errorf("payments: insert: %w", err)
	}

	return nil
}

// FindByReference loads an attempt by its reference.
func (r *MongoRepository) FindByReference(ctx context.Context, reference string) (*domain.Payment, error) {
	return r.findOne(ctx, bson.M{"reference": reference})
}

// FindByID loads an attempt by identifier.
func (r *MongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Payment, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// findOne is the shared lookup, mapping "no documents" to the domain error.
func (r *MongoRepository) findOne(ctx context.Context, filter bson.M) (*domain.Payment, error) {
	var payment domain.Payment
	if err := r.collection.FindOne(ctx, filter).Decode(&payment); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrPaymentNotFound
		}
		return nil, fmt.Errorf("payments: find: %w", err)
	}
	return &payment, nil
}

// ListForUser returns a customer's transaction history, newest first.
func (r *MongoRepository) ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]domain.Payment, int64, error) {
	return r.list(ctx, bson.M{"user_id": userID}, page, limit)
}

// List returns a filtered page for the admin dashboard.
func (r *MongoRepository) List(ctx context.Context, filter domain.Filter, page, limit int) ([]domain.Payment, int64, error) {
	query := bson.M{}

	if filter.UserID != nil {
		query["user_id"] = *filter.UserID
	}
	if filter.OrderID != nil {
		query["order_id"] = *filter.OrderID
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.Search != "" {
		pattern := bson.M{"$regex": regexp.QuoteMeta(filter.Search), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"reference": pattern},
			bson.M{"order_number": pattern},
			// A consultation payment has no order number, so the booking
			// reference is what an admin would search it by.
			bson.M{"booking_reference": pattern},
			bson.M{"customer_name": pattern},
			bson.M{"email": pattern},
		}
	}

	return r.list(ctx, query, page, limit)
}

// list runs a paginated, newest-first query.
func (r *MongoRepository) list(ctx context.Context, query bson.M, page, limit int) ([]domain.Payment, int64, error) {
	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("payments: count: %w", err)
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("payments: list: %w", err)
	}
	defer cursor.Close(ctx)

	payments := make([]domain.Payment, 0, limit)
	if err := cursor.All(ctx, &payments); err != nil {
		return nil, 0, fmt.Errorf("payments: decode: %w", err)
	}

	return payments, total, nil
}

// Settle records the provider's verdict, but only on an attempt that has not
// already reached a final state.
//
// The status filter is the idempotency guard: Paystack delivers a webhook at
// least once and the callback page verifies in parallel, so the same verdict
// routinely arrives twice. Only the first one applies.
func (r *MongoRepository) Settle(ctx context.Context, reference string, update domain.SettlementUpdate) (bool, error) {
	fields := bson.M{
		"status":     update.Status,
		"updated_at": time.Now().UTC(),
	}
	if update.ProviderReference != "" {
		fields["provider_reference"] = update.ProviderReference
	}
	if update.Channel != "" {
		fields["channel"] = update.Channel
	}
	if update.GatewayResponse != "" {
		fields["gateway_response"] = update.GatewayResponse
	}
	if update.PaidAt != nil {
		fields["paid_at"] = *update.PaidAt
	}

	result, err := r.collection.UpdateOne(ctx,
		bson.M{
			"reference": reference,
			"status":    bson.M{"$nin": bson.A{domain.StatusSuccessful, domain.StatusRefunded}},
		},
		bson.M{"$set": fields},
	)
	if err != nil {
		return false, fmt.Errorf("payments: settle: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// MarkRefunded flips a successful attempt to refunded.
func (r *MongoRepository) MarkRefunded(ctx context.Context, id bson.ObjectID) (bool, error) {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "status": domain.StatusSuccessful},
		bson.M{"$set": bson.M{
			"status":      domain.StatusRefunded,
			"refunded_at": now,
			"updated_at":  now,
		}},
	)
	if err != nil {
		return false, fmt.Errorf("payments: mark refunded: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// ReferenceExists reports whether a reference is taken.
func (r *MongoRepository) ReferenceExists(ctx context.Context, reference string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"reference": reference}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("payments: reference exists: %w", err)
	}
	return count > 0, nil
}
