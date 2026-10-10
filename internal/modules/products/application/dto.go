// Package application coordinates the catalogue workflows and assembles the
// read models the storefront and admin dashboard consume.
package application

import (
	"context"
	"time"

	categoriesdomain "github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	"github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Stock is the slice of the inventory read model the catalogue needs. The
// products module depends on this small port rather than on the inventory
// module's own types.
type Stock struct {
	Available int
	Reserved  int
	Sellable  int
	Badge     string
}

// StockPort is the catalogue's view of the inventory module.
type StockPort interface {
	// LevelsFor returns stock for a set of products, zero-filled for products
	// that have no record yet.
	LevelsFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]Stock, error)

	// EnsureRecord creates the stock record for a new product.
	EnsureRecord(ctx context.Context, productID bson.ObjectID) error

	// SetStock replaces a product's on-hand quantity.
	SetStock(ctx context.Context, productID bson.ObjectID, quantity int, actor StockActor) error

	// PurgeRecord forgets a deleted product's stock record and movement log.
	PurgeRecord(ctx context.Context, productID bson.ObjectID) error
}

// SalesPort tells the catalogue whether a product has ever been ordered,
// which is what decides between deleting it and archiving it.
type SalesPort interface {
	// HasOrdersFor reports whether any order contains the product.
	HasOrdersFor(ctx context.Context, productID bson.ObjectID) (bool, error)
}

// MediaPort is the catalogue's view of the media library, used to clean up
// the images of a product that is being deleted outright.
type MediaPort interface {
	// DeleteFile removes one file by its storage identifier.
	DeleteFile(ctx context.Context, fileID string) error
}

// RemoveResult reports which of the two outcomes a delete request produced.
type RemoveResult struct {
	// Deleted is true when the product was removed outright, false when it
	// was archived because something still refers to it.
	Deleted bool `json:"deleted"`
	// ImagesDeleted counts the media files removed alongside it.
	ImagesDeleted int `json:"images_deleted"`
}

// StockActor identifies the admin behind a stock change, without the
// catalogue depending on the inventory module's actor type.
type StockActor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}

// Media is a product image with the ImageKit file id admins need to manage it.
type Media struct {
	URL    string `json:"url"`
	FileID string `json:"file_id,omitempty"`
}

// View is the public projection of a product.
//
// `category` carries the slug because that is what the shop filters on, while
// `category_name` carries the label shown in the UI. `stock` is the sellable
// quantity: stock already reserved for unpaid orders is not offered twice.
type View struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	CategoryID   string   `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Price        int64    `json:"price"`
	Currency     string   `json:"currency"`
	Unit         string   `json:"unit"`
	Images       []string `json:"images"`
	Image        string   `json:"image,omitempty"`
	Media        []Media  `json:"media,omitempty"`
	Stock        int      `json:"stock"`
	StockBadge   string   `json:"stock_badge"`
	Status       string   `json:"status"`
	Featured     bool     `json:"featured"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// AdminView adds the stock breakdown admins need but customers do not see.
type AdminView struct {
	View
	AvailableQty int `json:"available_qty"`
	ReservedQty  int `json:"reserved_qty"`
}

// newView projects a product, its category and its stock into the read model.
func newView(product domain.Product, category *categoriesdomain.Category, stock Stock) View {
	images := make([]string, 0, len(product.Images))
	media := make([]Media, 0, len(product.Images))
	for _, image := range product.Images {
		images = append(images, image.URL)
		media = append(media, Media{URL: image.URL, FileID: image.FileID})
	}

	view := View{
		ID:          product.ID.Hex(),
		Name:        product.Name,
		Slug:        product.Slug,
		Description: product.Description,
		CategoryID:  product.CategoryID.Hex(),
		Price:       product.Price,
		Currency:    product.Currency,
		Unit:        product.Unit,
		Images:      images,
		Media:       media,
		Stock:       stock.Sellable,
		StockBadge:  stock.Badge,
		Status:      product.Status,
		Featured:    product.Featured,
		CreatedAt:   product.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   product.UpdatedAt.UTC().Format(time.RFC3339),
	}

	if len(images) > 0 {
		view.Image = images[0]
	}
	if category != nil {
		view.Category = category.Slug
		view.CategoryName = category.Name
	}

	return view
}

// ListInput is a product listing request.
type ListInput struct {
	CategorySlug string
	Status       string
	Featured     *bool
	Search       string
	Sort         string
	Page         int
	Limit        int
}

// CreateInput is an admin product creation request.
type CreateInput struct {
	Name         string
	Description  string
	CategorySlug string
	Price        int64
	Unit         string
	Images       []Media
	Status       string
	Featured     bool
	Stock        int
}

// UpdateInput is an admin product update. Nil fields are left unchanged, which
// is what makes this a PATCH rather than a replace.
type UpdateInput struct {
	Name         *string
	Description  *string
	CategorySlug *string
	Price        *int64
	Unit         *string
	Images       *[]Media
	Status       *string
	Featured     *bool
	Stock        *int
}
