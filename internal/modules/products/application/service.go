package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	categoriesdomain "github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	"github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/Dev-folabi/denisco_backend/internal/shared/slug"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Service implements the catalogue workflows.
type Service struct {
	products   domain.Repository
	categories categoriesdomain.Repository
	stock      StockPort
	sales      SalesPort
	media      MediaPort
	auditor    audit.Recorder
}

// NewService wires the catalogue to its repositories and the ports it needs
// to decide, and carry out, a product deletion.
func NewService(
	products domain.Repository,
	categories categoriesdomain.Repository,
	stock StockPort,
	sales SalesPort,
	media MediaPort,
	auditor audit.Recorder,
) *Service {
	return &Service{
		products:   products,
		categories: categories,
		stock:      stock,
		sales:      sales,
		media:      media,
		auditor:    auditor,
	}
}

// List returns a page of products with their categories and stock resolved.
//
// Callers that serve the storefront pass Status "active"; the admin listing
// leaves it empty to include archived products.
func (s *Service) List(ctx context.Context, input ListInput) ([]View, int64, error) {
	filter := domain.Filter{
		Status:   input.Status,
		Featured: input.Featured,
		Search:   strings.TrimSpace(input.Search),
		Sort:     domain.NormalizeSort(input.Sort),
	}

	if input.CategorySlug != "" {
		category, err := s.categories.FindBySlug(ctx, input.CategorySlug)
		if err != nil {
			if errors.Is(err, categoriesdomain.ErrCategoryNotFound) {
				// An unknown category is an empty result, not an error: the
				// shop's filter chips should not break on a stale link.
				return []View{}, 0, nil
			}
			return nil, 0, apperror.From(err)
		}
		filter.CategoryID = &category.ID
	}

	products, total, err := s.products.List(ctx, filter, input.Page, input.Limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	views, err := s.buildViews(ctx, products)
	if err != nil {
		return nil, 0, err
	}

	return views, total, nil
}

// ListAdmin returns the same page with the stock breakdown admins need.
func (s *Service) ListAdmin(ctx context.Context, input ListInput) ([]AdminView, int64, error) {
	views, total, err := s.List(ctx, input)
	if err != nil {
		return nil, 0, err
	}

	ids := make([]bson.ObjectID, 0, len(views))
	for _, view := range views {
		if id, err := bson.ObjectIDFromHex(view.ID); err == nil {
			ids = append(ids, id)
		}
	}

	levels, err := s.stock.LevelsFor(ctx, ids)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	adminViews := make([]AdminView, 0, len(views))
	for _, view := range views {
		adminView := AdminView{View: view}
		if id, err := bson.ObjectIDFromHex(view.ID); err == nil {
			level := levels[id]
			adminView.AvailableQty = level.Available
			adminView.ReservedQty = level.Reserved
		}
		adminViews = append(adminViews, adminView)
	}

	return adminViews, total, nil
}

// GetByID returns one product. publishedOnly hides archived products from the
// storefront.
func (s *Service) GetByID(ctx context.Context, id string, publishedOnly bool) (*View, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrProductNotFound
	}

	product, err := s.products.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	return s.single(ctx, product, publishedOnly)
}

// GetBySlug returns one product by its URL slug.
func (s *Service) GetBySlug(ctx context.Context, productSlug string, publishedOnly bool) (*View, error) {
	product, err := s.products.FindBySlug(ctx, productSlug)
	if err != nil {
		return nil, apperror.From(err)
	}

	return s.single(ctx, product, publishedOnly)
}

// single resolves one product into a view, enforcing visibility.
func (s *Service) single(ctx context.Context, product *domain.Product, publishedOnly bool) (*View, error) {
	if publishedOnly && !product.IsPurchasable() {
		return nil, domain.ErrProductNotFound
	}

	views, err := s.buildViews(ctx, []domain.Product{*product})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, domain.ErrProductNotFound
	}

	return &views[0], nil
}

// Related returns other active products in the same category, for the product
// detail page.
func (s *Service) Related(ctx context.Context, productID, categorySlug string, limit int) ([]View, error) {
	views, _, err := s.List(ctx, ListInput{
		CategorySlug: categorySlug,
		Status:       domain.StatusActive,
		Sort:         domain.SortFeatured,
		Page:         1,
		Limit:        limit + 1, // fetch one extra to drop the product itself
	})
	if err != nil {
		return nil, err
	}

	related := make([]View, 0, limit)
	for _, view := range views {
		if view.ID == productID {
			continue
		}
		if len(related) == limit {
			break
		}
		related = append(related, view)
	}

	return related, nil
}

// Create adds a product and opens its stock record.
func (s *Service) Create(ctx context.Context, input CreateInput, actor StockActor) (*View, error) {
	name, err := domain.ValidateName(input.Name)
	if err != nil {
		return nil, err
	}

	description, err := domain.ValidateDescription(input.Description)
	if err != nil {
		return nil, err
	}

	unit, err := domain.ValidateUnit(input.Unit)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidatePrice(input.Price); err != nil {
		return nil, err
	}

	status := input.Status
	if status == "" {
		status = domain.StatusActive
	}
	if status, err = domain.ValidateStatus(status); err != nil {
		return nil, err
	}

	category, err := s.categories.FindBySlug(ctx, strings.TrimSpace(input.CategorySlug))
	if err != nil {
		if errors.Is(err, categoriesdomain.ErrCategoryNotFound) {
			return nil, apperror.Validation.WithDetails(map[string]any{"category": "is not a known category"})
		}
		return nil, apperror.From(err)
	}

	if input.Stock < 0 {
		return nil, apperror.Validation.WithDetails(map[string]any{"stock": "cannot be negative"})
	}

	productSlug, err := slug.MakeUnique(name, func(candidate string) (bool, error) {
		return s.products.SlugExists(ctx, candidate)
	})
	if err != nil {
		return nil, apperror.Validation.WithDetails(map[string]any{"name": "could not be turned into a URL slug"}).Wrap(err)
	}

	now := time.Now().UTC()
	product := &domain.Product{
		ID:          bson.NewObjectID(),
		Name:        name,
		Slug:        productSlug,
		Description: description,
		CategoryID:  category.ID,
		Price:       input.Price,
		Currency:    domain.Currency,
		Unit:        unit,
		Images:      toDomainImages(input.Images),
		Status:      status,
		Featured:    input.Featured,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.products.Create(ctx, product); err != nil {
		return nil, apperror.From(err)
	}

	if err := s.stock.EnsureRecord(ctx, product.ID); err != nil {
		return nil, apperror.From(err)
	}
	if input.Stock > 0 {
		if err := s.stock.SetStock(ctx, product.ID, input.Stock, actor); err != nil {
			return nil, apperror.From(err)
		}
	}

	s.audit(ctx, actor, audit.ActionProductCreated, product.ID.Hex(), map[string]any{
		"name":  product.Name,
		"price": product.Price,
	})

	logger.FromContext(ctx).Info("product created",
		slog.String("module", "products"),
		slog.String("product_id", product.ID.Hex()),
		slog.String("slug", product.Slug),
	)

	return s.single(ctx, product, false)
}

// Update applies a partial change. The slug follows the name so shop links
// stay meaningful, and stock is delegated to the inventory module.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput, actor StockActor) (*View, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrProductNotFound
	}

	existing, err := s.products.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	fields := bson.M{}

	if input.Name != nil {
		name, err := domain.ValidateName(*input.Name)
		if err != nil {
			return nil, err
		}
		if name != existing.Name {
			newSlug, err := slug.MakeUnique(name, func(candidate string) (bool, error) {
				if candidate == existing.Slug {
					return false, nil
				}
				return s.products.SlugExists(ctx, candidate)
			})
			if err != nil {
				return nil, apperror.Validation.WithDetails(map[string]any{"name": "could not be turned into a URL slug"}).Wrap(err)
			}
			fields["name"] = name
			fields["slug"] = newSlug
		}
	}

	if input.Description != nil {
		description, err := domain.ValidateDescription(*input.Description)
		if err != nil {
			return nil, err
		}
		fields["description"] = description
	}

	if input.Unit != nil {
		unit, err := domain.ValidateUnit(*input.Unit)
		if err != nil {
			return nil, err
		}
		fields["unit"] = unit
	}

	if input.Price != nil {
		if err := domain.ValidatePrice(*input.Price); err != nil {
			return nil, err
		}
		fields["price"] = *input.Price
	}

	if input.Status != nil {
		status, err := domain.ValidateStatus(*input.Status)
		if err != nil {
			return nil, err
		}
		fields["status"] = status
	}

	if input.Featured != nil {
		fields["featured"] = *input.Featured
	}

	if input.Images != nil {
		fields["images"] = toDomainImages(*input.Images)
	}

	if input.CategorySlug != nil {
		category, err := s.categories.FindBySlug(ctx, strings.TrimSpace(*input.CategorySlug))
		if err != nil {
			if errors.Is(err, categoriesdomain.ErrCategoryNotFound) {
				return nil, apperror.Validation.WithDetails(map[string]any{"category": "is not a known category"})
			}
			return nil, apperror.From(err)
		}
		fields["category_id"] = category.ID
	}

	if err := s.products.Update(ctx, objectID, fields); err != nil {
		return nil, apperror.From(err)
	}

	if input.Stock != nil {
		if *input.Stock < 0 {
			return nil, apperror.Validation.WithDetails(map[string]any{"stock": "cannot be negative"})
		}
		if err := s.stock.SetStock(ctx, objectID, *input.Stock, actor); err != nil {
			return nil, apperror.From(err)
		}
	}

	s.audit(ctx, actor, audit.ActionProductUpdated, objectID.Hex(), map[string]any{
		"fields": fieldNames(fields),
	})

	return s.GetByID(ctx, id, false)
}

// Remove deletes a product, or archives it when something still refers to it.
//
// A product that was never ordered is a catalogue mistake — a typo, a
// duplicate, something added and thought better of — and leaving those behind
// as archived rows makes the catalogue harder to read for no gain. One that
// has been bought is a different thing: its order lines name it, and the
// customer's own order history has to keep resolving, so it is archived
// instead, which is what every earlier release did.
//
// The order check and the delete are not one atomic step, and deliberately
// so. An order placed in the instant between them would be an order for a
// product the admin had just asked to remove, and that order's lines already
// carry their own copy of the name, price, unit and image — the only thing
// lost is the link back to a catalogue entry that was on its way out anyway.
// Paying for a transaction here would buy very little.
func (s *Service) Remove(ctx context.Context, id string, actor StockActor) (*RemoveResult, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrProductNotFound
	}

	product, err := s.products.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	ordered, err := s.sales.HasOrdersFor(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	if ordered {
		return s.archive(ctx, product, actor)
	}

	return s.delete(ctx, product, actor)
}

// archive takes a product out of the shop without deleting its history.
func (s *Service) archive(ctx context.Context, product *domain.Product, actor StockActor) (*RemoveResult, error) {
	if product.Status == domain.StatusArchived {
		return &RemoveResult{}, nil
	}

	if err := s.products.Update(ctx, product.ID, bson.M{"status": domain.StatusArchived}); err != nil {
		return nil, apperror.From(err)
	}

	s.audit(ctx, actor, audit.ActionProductArchived, product.ID.Hex(), map[string]any{
		"reason": "product has orders",
	})

	logger.FromContext(ctx).Info("product archived",
		slog.String("module", "products"),
		slog.String("product_id", product.ID.Hex()),
	)

	return &RemoveResult{}, nil
}

// delete removes a product, its stock record and its images.
//
// The order matters: the images go last. A media call that fails must not
// leave a product row behind that an admin has already been told is gone,
// whereas an image left in the library is a tidy-up job and not a broken
// catalogue. For the same reason a failed image delete is logged rather than
// returned — the product is already gone by then, so reporting an error
// would describe the wrong outcome.
func (s *Service) delete(ctx context.Context, product *domain.Product, actor StockActor) (*RemoveResult, error) {
	log := logger.FromContext(ctx)

	if err := s.products.Delete(ctx, product.ID); err != nil {
		return nil, apperror.From(err)
	}

	if err := s.stock.PurgeRecord(ctx, product.ID); err != nil {
		// The product is gone; an orphan stock record is invisible to every
		// screen and is not worth failing the request over.
		log.Error("could not purge the stock record of a deleted product",
			slog.String("module", "products"),
			slog.String("product_id", product.ID.Hex()),
			slog.String("error", err.Error()),
		)
	}

	result := &RemoveResult{Deleted: true}

	for _, image := range product.Images {
		if image.FileID == "" {
			// Seeded and hand-entered images carry a URL but no file id, so
			// there is nothing to call the media library with.
			continue
		}
		if err := s.media.DeleteFile(ctx, image.FileID); err != nil {
			log.Error("could not delete a product image from the media library",
				slog.String("module", "products"),
				slog.String("product_id", product.ID.Hex()),
				slog.String("file_id", image.FileID),
				slog.String("error", err.Error()),
			)
			continue
		}
		result.ImagesDeleted++
	}

	s.audit(ctx, actor, audit.ActionProductDeleted, product.ID.Hex(), map[string]any{
		"name":           product.Name,
		"slug":           product.Slug,
		"images_deleted": result.ImagesDeleted,
	})

	log.Info("product deleted",
		slog.String("module", "products"),
		slog.String("product_id", product.ID.Hex()),
		slog.String("slug", product.Slug),
		slog.Int("images_deleted", result.ImagesDeleted),
	)

	return result, nil
}

// CountActive returns the number of listed products, for the admin dashboard.
func (s *Service) CountActive(ctx context.Context) (int64, error) {
	count, err := s.products.Count(ctx, domain.StatusActive)
	if err != nil {
		return 0, apperror.From(err)
	}
	return count, nil
}

// buildViews resolves categories and stock for a set of products in two
// queries rather than two per product.
func (s *Service) buildViews(ctx context.Context, products []domain.Product) ([]View, error) {
	if len(products) == 0 {
		return []View{}, nil
	}

	categories, err := s.categories.List(ctx, false)
	if err != nil {
		return nil, apperror.From(err)
	}
	byID := make(map[bson.ObjectID]categoriesdomain.Category, len(categories))
	for _, category := range categories {
		byID[category.ID] = category
	}

	ids := make([]bson.ObjectID, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}

	levels, err := s.stock.LevelsFor(ctx, ids)
	if err != nil {
		return nil, apperror.From(err)
	}

	views := make([]View, 0, len(products))
	for _, product := range products {
		var category *categoriesdomain.Category
		if found, ok := byID[product.CategoryID]; ok {
			category = &found
		}
		views = append(views, newView(product, category, levels[product.ID]))
	}

	return views, nil
}

// audit records an administrative catalogue change.
func (s *Service) audit(ctx context.Context, actor StockActor, action, resourceID string, metadata map[string]any) {
	if s.auditor == nil || actor.ID.IsZero() {
		return
	}

	id := actor.ID
	s.auditor.Record(ctx, audit.Entry{
		ActorID:    &id,
		ActorEmail: actor.Email,
		ActorRole:  actor.Role,
		Action:     action,
		Resource:   "product",
		ResourceID: resourceID,
		Metadata:   metadata,
	})
}

// toDomainImages converts the transport image shape into the stored one.
func toDomainImages(media []Media) []domain.Image {
	images := make([]domain.Image, 0, len(media))
	for _, item := range media {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		images = append(images, domain.Image{URL: item.URL, FileID: item.FileID})
	}
	return images
}

// fieldNames lists the changed field names for the audit entry, without their
// values.
func fieldNames(fields bson.M) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

// Lookup returns raw product records for a set of identifiers, keyed by id.
//
// Other modules use this to price carts and orders, so it deliberately returns
// the stored product rather than the storefront view: checkout must see the
// current price and status, not a cached projection.
func (s *Service) Lookup(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]domain.Product, error) {
	products, err := s.products.FindByIDs(ctx, ids)
	if err != nil {
		return nil, apperror.From(err)
	}

	found := make(map[bson.ObjectID]domain.Product, len(products))
	for _, product := range products {
		found[product.ID] = product
	}

	return found, nil
}
