// Package domain holds the product entity, its validation rules and the
// repository contract.
package domain

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Publication status. Archiving is a soft delete: an archived product keeps
// its history and stays resolvable from past orders, but leaves the shop.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Currency is fixed to naira; amounts are stored in kobo.
const Currency = "NGN"

// Sort keys accepted by the product listing, matching the shop's dropdown.
const (
	SortFeatured  = "featured"
	SortPriceAsc  = "price-asc"
	SortPriceDesc = "price-desc"
	SortName      = "name"
	SortNewest    = "newest"
)

// Product errors.
var (
	ErrProductNotFound = apperror.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "The product could not be found.")
	ErrSlugTaken       = apperror.New(http.StatusConflict, "PRODUCT_SLUG_TAKEN", "A product with this name already exists.")
	ErrProductInactive = apperror.New(http.StatusUnprocessableEntity, "PRODUCT_UNAVAILABLE", "This product is no longer available.")
)

// Image is a product photo held in ImageKit.
type Image struct {
	URL    string `bson:"url"`
	FileID string `bson:"file_id,omitempty"`
}

// Product is a purchasable item in the shop.
//
// Price is in kobo, the naira's minor unit, so money is always integer
// arithmetic. Stock is not stored here: it lives in the inventory module,
// which owns reservations.
type Product struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	Name        string        `bson:"name"`
	Slug        string        `bson:"slug"`
	Description string        `bson:"description"`
	CategoryID  bson.ObjectID `bson:"category_id"`
	Price       int64         `bson:"price"`
	Currency    string        `bson:"currency"`
	Unit        string        `bson:"unit"`
	Images      []Image       `bson:"images"`
	Status      string        `bson:"status"`
	Featured    bool          `bson:"featured"`
	CreatedAt   time.Time     `bson:"created_at"`
	UpdatedAt   time.Time     `bson:"updated_at"`
}

// IsPurchasable reports whether the product may be added to a cart.
func (p *Product) IsPurchasable() bool {
	return p.Status == StatusActive
}

// ValidateName trims and bounds a product name.
func ValidateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "is required"})
	}
	if len(trimmed) > 120 {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "must be 120 characters or fewer"})
	}
	return trimmed, nil
}

// ValidateUnit trims and bounds the selling unit ("bird", "head", "100kg bag").
func ValidateUnit(unit string) (string, error) {
	trimmed := strings.TrimSpace(unit)
	if trimmed == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"unit": "is required"})
	}
	if len(trimmed) > 40 {
		return "", apperror.Validation.WithDetails(map[string]any{"unit": "must be 40 characters or fewer"})
	}
	return trimmed, nil
}

// ValidatePrice bounds the price in kobo. Zero is rejected: a free product
// would be an input mistake, not a business case.
func ValidatePrice(price int64) error {
	if price <= 0 {
		return apperror.Validation.WithDetails(map[string]any{"price": "must be greater than zero"})
	}
	// ₦100,000,000 in kobo — far above any farm product, so a value beyond it
	// is a unit mix-up (naira entered as kobo).
	if price > 10_000_000_000 {
		return apperror.Validation.WithDetails(map[string]any{"price": "is unrealistically large; amounts are in kobo"})
	}
	return nil
}

// ValidateDescription bounds the description.
func ValidateDescription(description string) (string, error) {
	trimmed := strings.TrimSpace(description)
	if len(trimmed) > 2000 {
		return "", apperror.Validation.WithDetails(map[string]any{"description": "must be 2000 characters or fewer"})
	}
	return trimmed, nil
}

// ValidateStatus checks a publication status.
func ValidateStatus(status string) (string, error) {
	switch status {
	case StatusActive, StatusArchived:
		return status, nil
	default:
		return "", apperror.Validation.WithDetails(map[string]any{"status": "must be \"active\" or \"archived\""})
	}
}

// NormalizeSort maps a requested sort key to a supported one, defaulting to
// the shop's "Featured" order.
func NormalizeSort(sort string) string {
	switch sort {
	case SortPriceAsc, SortPriceDesc, SortName, SortNewest:
		return sort
	default:
		return SortFeatured
	}
}

// Filter narrows a product listing.
type Filter struct {
	// CategoryID limits results to one category.
	CategoryID *bson.ObjectID
	// Status limits results to one publication status; empty means any.
	Status string
	// Featured limits results to featured products when set.
	Featured *bool
	// Search matches the name and description.
	Search string
	Sort   string
}

// Repository is the persistence contract for products.
type Repository interface {
	// Create inserts a product, returning ErrSlugTaken on a slug collision.
	Create(ctx context.Context, product *Product) error

	// Update applies a set of changed fields.
	Update(ctx context.Context, id bson.ObjectID, fields bson.M) error

	// FindByID returns a product or ErrProductNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Product, error)

	// FindBySlug returns a product or ErrProductNotFound.
	FindBySlug(ctx context.Context, slug string) (*Product, error)

	// FindByIDs returns the given products, for building order and cart views.
	FindByIDs(ctx context.Context, ids []bson.ObjectID) ([]Product, error)

	// List returns a filtered, sorted page with the total match count.
	List(ctx context.Context, filter Filter, page, limit int) ([]Product, int64, error)

	// SlugExists reports whether a slug is already taken.
	SlugExists(ctx context.Context, slug string) (bool, error)

	// Count returns the number of products matching a status.
	Count(ctx context.Context, status string) (int64, error)

	// Delete removes a product outright. The caller only does this for a
	// product nothing refers to; everything else is archived instead.
	Delete(ctx context.Context, id bson.ObjectID) error
}

// PrimaryImage returns the product's first image URL, or an empty string when
// it has none. Carts and orders snapshot this so a line keeps its picture even
// after the catalogue entry changes.
func (p *Product) PrimaryImage() string {
	if len(p.Images) == 0 {
		return ""
	}
	return p.Images[0].URL
}
