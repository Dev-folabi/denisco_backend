// Package database owns the MongoDB client lifecycle and the version-controlled
// index definitions applied at startup.
package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// opTimeout bounds startup operations:
// connect verification, index creation and shutdown.
const opTimeout = 10 * time.Second

// Collection names. Every module references these constants rather than
// repeating string literals.
const (
	CollUsers                = "users"
	CollRefreshTokens        = "refresh_tokens"
	CollPasswordResets       = "password_resets"
	CollProducts             = "products"
	CollCategories           = "categories"
	CollCarts                = "carts"
	CollOrders               = "orders"
	CollPayments             = "payments"
	CollInventory            = "inventory"
	CollInventoryMovements   = "inventory_movements"
	CollConsultationTypes    = "consultation_types"
	CollConsultationSlots    = "consultation_slots"
	CollConsultationBookings = "consultation_bookings"
	CollOutboxEvents         = "outbox_events"
	CollAuditLogs            = "audit_logs"
)

// Connect builds the client and proves it works with a ping before returning.
// TLS is negotiated through the connection string (mongodb+srv), which Atlas
// requires; the URI must never be logged because it carries credentials.
func Connect(uri string, maxPoolSize uint64) (*mongo.Client, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(maxPoolSize).
		SetMinPoolSize(1).
		SetMaxConnIdleTime(5 * time.Minute).
		SetServerSelectionTimeout(opTimeout)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongo: create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}

	return client, nil
}

// Ping verifies the primary is reachable, for readiness probes.
func Ping(ctx context.Context, client *mongo.Client) error {
	if client == nil {
		return fmt.Errorf("mongo: client not configured")
	}
	return client.Ping(ctx, readpref.Primary())
}

// Disconnect releases every pooled connection gracefully.
func Disconnect(client *mongo.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return client.Disconnect(ctx)
}
