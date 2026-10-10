package application

import (
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// Cart-facing errors raised against the catalogue and stock ledger.
var (
	ErrProductNotFound    = apperror.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "The product could not be found.")
	ErrProductUnavailable = apperror.New(http.StatusUnprocessableEntity, "PRODUCT_UNAVAILABLE", "This product is no longer available.")
	ErrInsufficientStock  = apperror.New(http.StatusConflict, "INSUFFICIENT_STOCK", "There is not enough stock available for this product.")
)

// ItemView is one priced cart line.
//
// ID repeats ProductID: a cart holds at most one line per product, so the
// product is the line's natural identifier and clients can address a line by
// either name.
type ItemView struct {
	ID         string `json:"id"`
	ProductID  string `json:"product_id"`
	Name       string `json:"name"`
	Slug       string `json:"slug,omitempty"`
	Image      string `json:"image,omitempty"`
	Unit       string `json:"unit,omitempty"`
	UnitPrice  int64  `json:"unit_price"`
	Quantity   int    `json:"quantity"`
	LineTotal  int64  `json:"line_total"`
	Stock      int    `json:"stock"`
	StockBadge string `json:"stock_badge,omitempty"`
	// Available is false when the product was archived or its stock dropped
	// below the quantity in the cart.
	Available bool      `json:"available"`
	AddedAt   time.Time `json:"added_at"`
}

// View is the priced cart returned to the customer.
type View struct {
	Items     []ItemView `json:"items"`
	Subtotal  int64      `json:"subtotal"`
	ItemCount int        `json:"item_count"`
	Currency  string     `json:"currency"`
	// HasUnavailableItems tells the storefront to block checkout until the
	// customer resolves the offending lines.
	HasUnavailableItems bool `json:"has_unavailable_items"`
}
