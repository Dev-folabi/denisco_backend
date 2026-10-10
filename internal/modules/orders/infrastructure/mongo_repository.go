// Package infrastructure holds the MongoDB implementation of the order
// repository.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists orders in the `orders` collection.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollOrders)}
}

// Create inserts an order.
func (r *MongoRepository) Create(ctx context.Context, order *domain.Order) error {
	if order.ID.IsZero() {
		order.ID = bson.NewObjectID()
	}

	if _, err := r.collection.InsertOne(ctx, order); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// The order number collided; the caller retries with a new one.
			return fmt.Errorf("orders: duplicate order number: %w", err)
		}
		return fmt.Errorf("orders: insert: %w", err)
	}

	return nil
}

// FindByID loads an order by identifier.
func (r *MongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Order, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// FindByNumber loads an order by its customer-facing number.
func (r *MongoRepository) FindByNumber(ctx context.Context, number string) (*domain.Order, error) {
	return r.findOne(ctx, bson.M{"order_number": number})
}

// FindByPaymentRef loads the order a payment reference belongs to.
func (r *MongoRepository) FindByPaymentRef(ctx context.Context, reference string) (*domain.Order, error) {
	return r.findOne(ctx, bson.M{"payment_ref": reference})
}

// findOne is the shared lookup, mapping "no documents" to the domain error.
func (r *MongoRepository) findOne(ctx context.Context, filter bson.M) (*domain.Order, error) {
	var order domain.Order
	if err := r.collection.FindOne(ctx, filter).Decode(&order); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, fmt.Errorf("orders: find: %w", err)
	}
	return &order, nil
}

// ListForUser returns a customer's order history, newest first.
func (r *MongoRepository) ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]domain.Order, int64, error) {
	return r.list(ctx, bson.M{"user_id": userID}, page, limit)
}

// List returns a filtered page for the admin dashboard.
func (r *MongoRepository) List(ctx context.Context, filter domain.Filter, page, limit int) ([]domain.Order, int64, error) {
	query := bson.M{}

	if filter.UserID != nil {
		query["user_id"] = *filter.UserID
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.PaymentStatus != "" {
		query["payment_status"] = filter.PaymentStatus
	}
	if filter.NeedsReconciliation != nil {
		if *filter.NeedsReconciliation {
			query["needs_reconciliation"] = true
		} else {
			// The flag is omitted entirely on the orders that never needed
			// one, so "not flagged" cannot be a plain equality match.
			query["needs_reconciliation"] = bson.M{"$ne": true}
		}
	}
	if filter.Search != "" {
		pattern := bson.M{"$regex": regexp.QuoteMeta(filter.Search), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"order_number": pattern},
			bson.M{"customer.name": pattern},
			bson.M{"customer.email": pattern},
		}
	}

	return r.list(ctx, query, page, limit)
}

// list runs a paginated, newest-first query.
func (r *MongoRepository) list(ctx context.Context, query bson.M, page, limit int) ([]domain.Order, int64, error) {
	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("orders: count: %w", err)
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("orders: list: %w", err)
	}
	defer cursor.Close(ctx)

	orders := make([]domain.Order, 0, limit)
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, 0, fmt.Errorf("orders: decode: %w", err)
	}

	return orders, total, nil
}

// Update applies changed fields and stamps updated_at.
func (r *MongoRepository) Update(ctx context.Context, id bson.ObjectID, fields bson.M) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()

	result, err := r.collection.UpdateByID(ctx, id, bson.M{"$set": fields})
	if err != nil {
		return fmt.Errorf("orders: update: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrOrderNotFound
	}

	return nil
}

// MarkPaid flips the order to paid, but only while nothing has settled yet.
//
// The payment_status condition is what makes a replayed Paystack webhook
// harmless: the second delivery matches nothing and reports false, so stock is
// never deducted twice. A failed attempt is included because a customer whose
// card was declined may pay with another one against the same order.
func (r *MongoRepository) MarkPaid(ctx context.Context, id bson.ObjectID, reference string, paidAt time.Time) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "payment_status": bson.M{"$in": bson.A{domain.PaymentPending, domain.PaymentFailed}}},
		bson.M{"$set": bson.M{
			"payment_status": domain.PaymentPaid,
			"payment_ref":    reference,
			"paid_at":        paidAt,
			"updated_at":     time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, fmt.Errorf("orders: mark paid: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// ReleaseReservation clears the reservation flag, but only while it is set, so
// the stock behind it is returned exactly once.
func (r *MongoRepository) ReleaseReservation(ctx context.Context, id bson.ObjectID) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "stock_reserved": true},
		bson.M{"$set": bson.M{"stock_reserved": false, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return false, fmt.Errorf("orders: release reservation: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// NumberExists reports whether an order number is taken.
func (r *MongoRepository) NumberExists(ctx context.Context, number string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"order_number": number}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("orders: number exists: %w", err)
	}
	return count > 0, nil
}

// CountForProduct reports how many orders contain a product.
//
// It counts every order, cancelled ones included: an order line names the
// product it was bought as, and deleting the product would leave that line
// pointing at nothing.
func (r *MongoRepository) CountForProduct(ctx context.Context, productID bson.ObjectID) (int64, error) {
	count, err := r.collection.CountDocuments(ctx,
		bson.M{"items.product_id": productID}, options.Count().SetLimit(1))
	if err != nil {
		return 0, fmt.Errorf("orders: count for product: %w", err)
	}
	return count, nil
}

// Stats aggregates the order figures shown on the admin dashboard.
func (r *MongoRepository) Stats(ctx context.Context) (domain.Stats, error) {
	var stats domain.Stats

	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return stats, fmt.Errorf("orders: count all: %w", err)
	}
	stats.TotalOrders = total

	pending, err := r.collection.CountDocuments(ctx, bson.M{"status": domain.StatusPending})
	if err != nil {
		return stats, fmt.Errorf("orders: count pending: %w", err)
	}
	stats.PendingOrders = pending

	paid, err := r.collection.CountDocuments(ctx, bson.M{"payment_status": domain.PaymentPaid})
	if err != nil {
		return stats, fmt.Errorf("orders: count paid: %w", err)
	}
	stats.PaidOrders = paid

	// Revenue counts paid orders only: an unpaid order is not money.
	cursor, err := r.collection.Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{"payment_status": domain.PaymentPaid}},
		bson.M{"$group": bson.M{"_id": nil, "revenue": bson.M{"$sum": "$total"}}},
	})
	if err != nil {
		return stats, fmt.Errorf("orders: revenue: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Revenue int64 `bson:"revenue"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return stats, fmt.Errorf("orders: decode revenue: %w", err)
	}
	if len(rows) > 0 {
		stats.Revenue = rows[0].Revenue
	}

	return stats, nil
}

// Recent returns the newest orders for the admin dashboard.
func (r *MongoRepository) Recent(ctx context.Context, limit int) ([]domain.Order, error) {
	cursor, err := r.collection.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("orders: recent: %w", err)
	}
	defer cursor.Close(ctx)

	orders := make([]domain.Order, 0, limit)
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, fmt.Errorf("orders: decode recent: %w", err)
	}

	return orders, nil
}

// DailyRevenue sums paid order totals per calendar day since a cut-off.
//
// Days are grouped in Africa/Lagos so the chart's bars line up with the farm's
// trading days rather than with UTC.
func (r *MongoRepository) DailyRevenue(ctx context.Context, since time.Time) (map[string]int64, error) {
	cursor, err := r.collection.Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{
			"payment_status": domain.PaymentPaid,
			"created_at":     bson.M{"$gte": since},
		}},
		bson.M{"$group": bson.M{
			"_id": bson.M{"$dateToString": bson.M{
				"format":   "%Y-%m-%d",
				"date":     "$created_at",
				"timezone": "Africa/Lagos",
			}},
			"revenue": bson.M{"$sum": "$total"},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("orders: daily revenue: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Date    string `bson:"_id"`
		Revenue int64  `bson:"revenue"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("orders: decode daily revenue: %w", err)
	}

	revenue := make(map[string]int64, len(rows))
	for _, row := range rows {
		revenue[row.Date] = row.Revenue
	}

	return revenue, nil
}

// StaleReserved returns unpaid orders that are still holding stock past the
// cut-off, so the worker can return those units to the shop.
func (r *MongoRepository) StaleReserved(ctx context.Context, before time.Time, limit int) ([]domain.Order, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"stock_reserved": true,
		"payment_status": domain.PaymentPending,
		"status":         bson.M{"$ne": domain.StatusCancelled},
		"created_at":     bson.M{"$lt": before},
	}, options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("orders: stale reserved: %w", err)
	}
	defer cursor.Close(ctx)

	orders := make([]domain.Order, 0, limit)
	if err := cursor.All(ctx, &orders); err != nil {
		return nil, fmt.Errorf("orders: decode stale reserved: %w", err)
	}

	return orders, nil
}
