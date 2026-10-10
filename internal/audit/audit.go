// Package audit records sensitive administrative actions.
//
// The architecture requires an immutable trail for inventory adjustments,
// order transitions, refunds and catalogue changes: entries are only ever
// appended, never updated or deleted.
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Actions recorded by the admin workflows.
const (
	ActionProductCreated        = "product.created"
	ActionProductUpdated        = "product.updated"
	ActionProductArchived       = "product.archived"
	ActionProductDeleted        = "product.deleted"
	ActionInventoryAdjusted     = "inventory.adjusted"
	ActionOrderStatusChanged    = "order.status_changed"
	ActionOrderLatePayment      = "order.late_payment"
	ActionPaymentRefunded       = "payment.refunded"
	ActionUploadSignatureIssued = "media.upload_signature_issued"
)

// ActorSystem attributes an entry to the platform itself, used where a rule
// fired without an administrator behind it — late-payment reconciliation, for
// one.
const ActorSystem = "system"

// Entry is one recorded administrative action.
type Entry struct {
	ID         bson.ObjectID  `bson:"_id,omitempty"`
	ActorID    *bson.ObjectID `bson:"actor_id,omitempty"`
	ActorEmail string         `bson:"actor_email,omitempty"`
	ActorRole  string         `bson:"actor_role,omitempty"`
	Action     string         `bson:"action"`
	Resource   string         `bson:"resource"`
	ResourceID string         `bson:"resource_id,omitempty"`
	Metadata   map[string]any `bson:"metadata,omitempty"`
	CreatedAt  time.Time      `bson:"created_at"`
}

// Recorder appends audit entries.
type Recorder interface {
	Record(ctx context.Context, entry Entry)
}

// MongoRecorder is the MongoDB-backed Recorder.
type MongoRecorder struct {
	collection *mongo.Collection
}

// NewMongoRecorder wires the recorder to a database handle.
func NewMongoRecorder(db *mongo.Database) *MongoRecorder {
	return &MongoRecorder{collection: db.Collection(database.CollAuditLogs)}
}

// Record appends an entry. A failure is logged rather than returned: the
// administrative action it describes has already succeeded, and failing the
// request afterwards would be worse than a missing log line.
func (r *MongoRecorder) Record(ctx context.Context, entry Entry) {
	entry.ID = bson.NewObjectID()
	entry.CreatedAt = time.Now().UTC()

	if _, err := r.collection.InsertOne(ctx, entry); err != nil {
		logger.FromContext(ctx).Error("could not record audit entry",
			slog.String("action", entry.Action),
			slog.String("resource_id", entry.ResourceID),
			slog.String("error", fmt.Sprintf("%v", err)),
		)
	}
}

// List returns a page of entries, newest first.
func (r *MongoRecorder) List(ctx context.Context, page, limit int) ([]Entry, int64, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	cursor, err := r.collection.Find(ctx, bson.M{},
		mongoFindOptions(page, limit),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: list: %w", err)
	}
	defer cursor.Close(ctx)

	entries := make([]Entry, 0, limit)
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, 0, fmt.Errorf("audit: decode: %w", err)
	}

	return entries, total, nil
}
