// Package domain holds the shopping-cart entity and its rules.
//
// A cart stores product references and quantities only. Prices are never
// stored here: they are read from the catalogue every time the cart is shown
// and again at checkout, so a price change can never be bypassed by leaving
// an item in a cart.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// MaxLineQuantity caps a single line, as a guard against input mistakes. Stock
// is the real limit and is checked separately.
const MaxLineQuantity = 999

// Cart errors.
var (
	ErrCartEmpty     = apperror.New(http.StatusUnprocessableEntity, "CART_EMPTY", "Your cart is empty.")
	ErrItemNotInCart = apperror.New(http.StatusNotFound, "CART_ITEM_NOT_FOUND", "That item is not in your cart.")
	ErrQuantity      = apperror.New(http.StatusUnprocessableEntity, "INVALID_QUANTITY", "The quantity must be between 1 and 999.")
)

// Item is one line in a cart, keyed by product: adding the same product twice
// increases its quantity rather than creating a second line.
type Item struct {
	ProductID bson.ObjectID `bson:"product_id"`
	Quantity  int           `bson:"quantity"`
	AddedAt   time.Time     `bson:"added_at"`
}

// Cart is a customer's working basket.
type Cart struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    bson.ObjectID `bson:"user_id"`
	Items     []Item        `bson:"items"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

// ProductIDs lists the products referenced by the cart.
func (c *Cart) ProductIDs() []bson.ObjectID {
	ids := make([]bson.ObjectID, 0, len(c.Items))
	for _, item := range c.Items {
		ids = append(ids, item.ProductID)
	}
	return ids
}

// ValidateQuantity bounds a requested line quantity.
func ValidateQuantity(quantity int) error {
	if quantity < 1 || quantity > MaxLineQuantity {
		return ErrQuantity
	}
	return nil
}

// Repository is the persistence contract for carts.
type Repository interface {
	// Get returns the customer's cart, or an empty one when they have none.
	Get(ctx context.Context, userID bson.ObjectID) (*Cart, error)

	// AddItem adds a product or increases its quantity.
	AddItem(ctx context.Context, userID, productID bson.ObjectID, quantity int) error

	// SetQuantity replaces a line's quantity, reporting false when the line is
	// not in the cart.
	SetQuantity(ctx context.Context, userID, productID bson.ObjectID, quantity int) (bool, error)

	// RemoveItem drops a line, reporting false when it was not there.
	RemoveItem(ctx context.Context, userID, productID bson.ObjectID) (bool, error)

	// Clear empties the cart, which checkout does once an order is placed.
	Clear(ctx context.Context, userID bson.ObjectID) error
}
