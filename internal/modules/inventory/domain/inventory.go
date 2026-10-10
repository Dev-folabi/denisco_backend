// Package domain holds the stock entity, its movement log and the rules that
// keep quantities consistent under concurrency.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Stock thresholds from the prototype, used for the shop's stock badges.
const (
	// LowStockThreshold is the inclusive upper bound for the amber badge.
	LowStockThreshold = 10
)

// Stock badge values returned to the storefront.
const (
	BadgeOutOfStock = "out_of_stock"
	BadgeLowStock   = "low_stock"
	BadgeInStock    = "in_stock"
)

// Movement types recorded in the audit trail.
const (
	MovementReservation = "reservation"
	MovementRelease     = "release"
	MovementDeduction   = "deduction"
	MovementAdjustment  = "adjustment"
)

// Inventory errors.
var (
	ErrInventoryNotFound = apperror.New(http.StatusNotFound, "INVENTORY_NOT_FOUND", "No stock record exists for this product.")
	ErrInsufficientStock = apperror.New(http.StatusConflict, "INSUFFICIENT_STOCK", "There is not enough stock available for this product.")
	ErrReasonRequired    = apperror.New(http.StatusUnprocessableEntity, "ADJUSTMENT_REASON_REQUIRED", "A reason is required for every stock adjustment.")
	ErrInvalidAdjustment = apperror.New(http.StatusUnprocessableEntity, "INVALID_ADJUSTMENT", "The adjustment would leave less stock than is already reserved.")
)

// Inventory is the stock record for one product.
//
// AvailableQty is the quantity physically on hand. ReservedQty is the part of
// it already promised to orders awaiting payment. Only the difference may be
// sold, which is what every stock check uses.
type Inventory struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	ProductID    bson.ObjectID `bson:"product_id"`
	AvailableQty int           `bson:"available_qty"`
	ReservedQty  int           `bson:"reserved_qty"`
	UpdatedAt    time.Time     `bson:"updated_at"`
}

// Sellable is the quantity a customer may still buy.
func (i *Inventory) Sellable() int {
	sellable := i.AvailableQty - i.ReservedQty
	if sellable < 0 {
		return 0
	}
	return sellable
}

// Badge classifies sellable stock for the storefront, following the
// prototype's thresholds.
func Badge(sellable int) string {
	switch {
	case sellable <= 0:
		return BadgeOutOfStock
	case sellable <= LowStockThreshold:
		return BadgeLowStock
	default:
		return BadgeInStock
	}
}

// Movement is an immutable record of a stock change, kept so every quantity
// can be explained after the fact.
type Movement struct {
	ID        bson.ObjectID  `bson:"_id,omitempty"`
	ProductID bson.ObjectID  `bson:"product_id"`
	Type      string         `bson:"type"`
	Quantity  int            `bson:"quantity"` // signed: the delta applied
	Reason    string         `bson:"reason,omitempty"`
	ActorID   *bson.ObjectID `bson:"actor_id,omitempty"`
	// Reference ties a movement to the order that caused it.
	Reference string    `bson:"reference,omitempty"`
	CreatedAt time.Time `bson:"created_at"`
}

// Repository is the persistence contract for stock.
//
// The mutating operations are deliberately conditional: each one succeeds only
// while its precondition still holds, so two concurrent callers cannot both
// claim the last unit.
type Repository interface {
	// EnsureRecord creates a zeroed record for a product if none exists.
	EnsureRecord(ctx context.Context, productID bson.ObjectID) error

	// Get returns the record or ErrInventoryNotFound.
	Get(ctx context.Context, productID bson.ObjectID) (*Inventory, error)

	// ListByProducts returns records for the given products, for building
	// read models without an N+1 query.
	ListByProducts(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]Inventory, error)

	// List returns every record, newest update first, with its total count.
	List(ctx context.Context, page, limit int) ([]Inventory, int64, error)

	// Reserve promises stock to an order. It reports false when the sellable
	// quantity is lower than requested.
	Reserve(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error)

	// Release returns reserved stock to the sellable pool.
	Release(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error)

	// Deduct removes paid-for stock from both the on-hand and reserved
	// quantities.
	Deduct(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error)

	// SellWithoutReservation removes stock that was never reserved, for a
	// payment that arrived after its reservation had been returned. It
	// reports false when there is no longer enough sellable stock.
	SellWithoutReservation(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error)

	// Adjust applies a signed correction to the on-hand quantity. It reports
	// false when the result would drop below what is already reserved.
	Adjust(ctx context.Context, productID bson.ObjectID, delta int) (bool, error)

	// SetAvailable replaces the on-hand quantity outright, used when an admin
	// edits a product's stock field.
	SetAvailable(ctx context.Context, productID bson.ObjectID, quantity int) (bool, error)

	// RecordMovement appends to the movement log.
	RecordMovement(ctx context.Context, movement *Movement) error

	// PurgeProduct removes a product's stock record and its movement log. It
	// is used only when the product itself is being deleted, which the
	// catalogue allows only while nothing refers to it — so there is no
	// history left to explain.
	PurgeProduct(ctx context.Context, productID bson.ObjectID) error

	// Movements returns the movement history, optionally for one product.
	Movements(ctx context.Context, productID *bson.ObjectID, page, limit int) ([]Movement, int64, error)
}
