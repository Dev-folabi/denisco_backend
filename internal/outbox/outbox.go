// Package outbox implements the write side of the transactional outbox. Domain
// events are committed to MongoDB alongside the state change that produced
// them; the outbox publisher process later hands them to Redis/Asynq.
//
// Writing the event here (rather than calling Resend inline) keeps request
// handling fast and makes delivery retryable.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Event statuses.
const (
	StatusPending   = "pending"
	StatusPublished = "published"
	StatusFailed    = "failed"
)

// Event types emitted by the auth module. Later phases add their own.
const (
	TypeUserRegistered         = "user.registered"
	TypePasswordResetRequested = "password_reset.requested"
	TypePasswordChanged        = "password.changed"
	// TypeContactReceived is a submission of the site's contact form. It is
	// addressed to farm staff alone: a visitor has written in, and there is
	// nobody else it concerns.
	TypeContactReceived = "contact.received"
)

// Event is a durable record of something that happened, awaiting publication.
type Event struct {
	ID          bson.ObjectID  `bson:"_id,omitempty"`
	Type        string         `bson:"type"`
	AggregateID string         `bson:"aggregate_id"`
	Payload     map[string]any `bson:"payload"`
	Status      string         `bson:"status"`
	Attempts    int            `bson:"attempts"`
	LastError   string         `bson:"last_error,omitempty"`
	CreatedAt   time.Time      `bson:"created_at"`
	ClaimedAt   *time.Time     `bson:"claimed_at,omitempty"`
	PublishedAt *time.Time     `bson:"published_at,omitempty"`
}

// Writer appends events to the outbox collection.
type Writer interface {
	Append(ctx context.Context, eventType, aggregateID string, payload map[string]any) error
}

// MongoWriter is the MongoDB-backed Writer.
type MongoWriter struct {
	collection *mongo.Collection
}

// NewMongoWriter wires the writer to a database handle.
func NewMongoWriter(db *mongo.Database) *MongoWriter {
	return &MongoWriter{collection: db.Collection(database.CollOutboxEvents)}
}

// Append records a pending event. The caller passes a context that may carry a
// session, so the insert can join the surrounding transaction once multi-
// document workflows arrive in later phases.
func (w *MongoWriter) Append(ctx context.Context, eventType, aggregateID string, payload map[string]any) error {
	event := Event{
		ID:          bson.NewObjectID(),
		Type:        eventType,
		AggregateID: aggregateID,
		Payload:     payload,
		Status:      StatusPending,
		CreatedAt:   time.Now().UTC(),
	}

	if _, err := w.collection.InsertOne(ctx, event); err != nil {
		return fmt.Errorf("outbox: append %s: %w", eventType, err)
	}

	return nil
}

// Event types emitted by the e-commerce and consultation modules. They are
// listed here so the publisher and worker share one vocabulary.
const (
	TypeOrderCreated       = "order.created"
	TypeOrderPaid          = "order.paid"
	TypeOrderStatusChanged = "order.status_changed"
	TypeOrderCancelled     = "order.cancelled"
	// TypeOrderPaymentUnreconciled goes to farm staff, not to the customer:
	// a payment was confirmed for an order whose stock had already been
	// returned to the shop and could not be secured again.
	TypeOrderPaymentUnreconciled = "order.payment_unreconciled"
	// TypeOrderRefunded and TypeOrderPaymentFailed tell the customer their
	// money is on its way back, or that an attempt did not go through and the
	// order is still payable.
	TypeOrderRefunded      = "order.refunded"
	TypeOrderPaymentFailed = "order.payment_failed"
	TypeConsultationBooked = "consultation.booked"
	// TypeConsultationCancelled is the customer cancelling, or an admin
	// cancelling on their behalf.
	TypeConsultationCancelled = "consultation.cancelled"
	TypeConsultationStatus    = "consultation.status_changed"
	// TypeConsultationRefunded returns a paid fee; TypeConsultationPaymentFailed
	// says a fee did not go through. Neither is copied to staff: a refund needs
	// no action from the farm, and a declined card is between the customer and
	// their bank.
	TypeConsultationRefunded      = "consultation.refunded"
	TypeConsultationPaymentFailed = "consultation.payment_failed"
)

// maxAttempts is how often publication is retried before an event is parked
// as failed for a human to look at.
const maxAttempts = 10

// Reader claims and settles pending events. The publisher process uses it;
// request handlers only ever append.
type Reader interface {
	// Claim atomically takes up to `limit` pending events, marking them as
	// in-flight so a second publisher cannot take the same ones.
	Claim(ctx context.Context, limit int, olderThan time.Duration) ([]Event, error)

	// MarkPublished records successful publication.
	MarkPublished(ctx context.Context, id bson.ObjectID) error

	// MarkFailed records a failed attempt, returning the event to the pending
	// pool until the attempt ceiling is reached.
	MarkFailed(ctx context.Context, id bson.ObjectID, reason string) error
}

// StatusPublishing marks an event a publisher has claimed but not yet
// confirmed, so a crash leaves a recoverable trail rather than a lost event.
const StatusPublishing = "publishing"

// Claim takes a batch of pending events for publication.
//
// Each claim is a conditional findOneAndUpdate, so two publishers running side
// by side never pick up the same event. Events stuck in `publishing` longer
// than `olderThan` are reclaimed: that is how a publisher crash recovers.
func (w *MongoWriter) Claim(ctx context.Context, limit int, olderThan time.Duration) ([]Event, error) {
	now := time.Now().UTC()
	stuckBefore := now.Add(-olderThan)

	claimed := make([]Event, 0, limit)

	for len(claimed) < limit {
		filter := bson.M{
			"attempts": bson.M{"$lt": maxAttempts},
			"$or": bson.A{
				bson.M{"status": StatusPending},
				bson.M{"status": StatusPublishing, "claimed_at": bson.M{"$lt": stuckBefore}},
			},
		}

		update := bson.M{
			"$set": bson.M{"status": StatusPublishing, "claimed_at": now},
			"$inc": bson.M{"attempts": 1},
		}

		var event Event
		err := w.collection.FindOneAndUpdate(ctx, filter, update,
			options.FindOneAndUpdate().
				SetSort(bson.D{{Key: "created_at", Value: 1}}).
				SetReturnDocument(options.After),
		).Decode(&event)

		if errors.Is(err, mongo.ErrNoDocuments) {
			break
		}
		if err != nil {
			return claimed, fmt.Errorf("outbox: claim: %w", err)
		}

		claimed = append(claimed, event)
	}

	return claimed, nil
}

// MarkPublished records successful publication.
func (w *MongoWriter) MarkPublished(ctx context.Context, id bson.ObjectID) error {
	now := time.Now().UTC()

	if _, err := w.collection.UpdateByID(ctx, id, bson.M{
		"$set":   bson.M{"status": StatusPublished, "published_at": now},
		"$unset": bson.M{"last_error": "", "claimed_at": ""},
	}); err != nil {
		return fmt.Errorf("outbox: mark published: %w", err)
	}

	return nil
}

// MarkFailed returns an event to the pending pool, or parks it once it has
// used up its attempts.
func (w *MongoWriter) MarkFailed(ctx context.Context, id bson.ObjectID, reason string) error {
	// Reading the attempt count back decides between retrying and parking.
	var event Event
	if err := w.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&event); err != nil {
		return fmt.Errorf("outbox: read for failure: %w", err)
	}

	status := StatusPending
	if event.Attempts >= maxAttempts {
		status = StatusFailed
	}

	if _, err := w.collection.UpdateByID(ctx, id, bson.M{
		"$set":   bson.M{"status": status, "last_error": reason},
		"$unset": bson.M{"claimed_at": ""},
	}); err != nil {
		return fmt.Errorf("outbox: mark failed: %w", err)
	}

	return nil
}
