// Package domain holds the product-category entity and its repository
// contract. Categories are a small, slow-changing reference set seeded from
// the prototype.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ErrCategoryNotFound is returned when a slug or id matches nothing.
var ErrCategoryNotFound = apperror.New(http.StatusNotFound, "CATEGORY_NOT_FOUND", "The category could not be found.")

// Category groups products in the shop's filter chips.
type Category struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	Name        string        `bson:"name"`
	Slug        string        `bson:"slug"`
	Description string        `bson:"description,omitempty"`
	// Position fixes the order of the shop's filter chips, which follows the
	// prototype rather than alphabetical order.
	Position  int       `bson:"position"`
	Active    bool      `bson:"active"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Seed returns the catalogue's categories, in display order.
//
// This supersedes the prototype's five categories at the client's request
// (2026-10-25): the piggery unit broadened into "Pig & Micro-Livestock"
// (keeping the slug "piggery" so existing records and URLs still resolve),
// and a "Spices & Nutrition" category was added for the AROMACRAFT and
// ROYAL LUSH blend ranges.
func Seed() []Category {
	return []Category{
		{Slug: "poultry", Name: "Poultry", Position: 1, Active: true},
		{Slug: "livestock", Name: "Livestock", Position: 2, Active: true},
		{Slug: "piggery", Name: "Pig & Micro-Livestock", Position: 3, Active: true},
		{Slug: "crops", Name: "Crop Farming", Position: 4, Active: true},
		{Slug: "snail", Name: "Snail Farming", Position: 5, Active: true},
		{Slug: "spices", Name: "Spices & Nutrition", Position: 6, Active: true},
	}
}

// Repository is the persistence contract for categories.
type Repository interface {
	// List returns categories in display order; activeOnly hides retired ones.
	List(ctx context.Context, activeOnly bool) ([]Category, error)

	// FindByID returns a category or ErrCategoryNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Category, error)

	// FindBySlug returns a category or ErrCategoryNotFound.
	FindBySlug(ctx context.Context, slug string) (*Category, error)

	// Upsert inserts or updates by slug, which is what the seed script uses.
	Upsert(ctx context.Context, category *Category) error
}
