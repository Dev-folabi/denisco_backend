// Command seed-catalogue loads the categories and products defined by
// denisco_prototype.html, together with their opening stock levels.
//
// The command is idempotent: it matches existing records by slug and updates
// them, so it can be re-run after a prototype change without creating
// duplicates. Pass -reset-stock to also reset on-hand quantities to the
// prototype's values.
//
// Usage:
//
//	go run ./scripts/seed-catalogue
//	go run ./scripts/seed-catalogue -reset-stock
//	go run ./scripts/seed-catalogue -create-only      # live database: add missing products only
//	go run ./scripts/seed-catalogue -media-base https://ik.imagekit.io/<id>/denisco
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	categoriesdomain "github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	categoriesinfra "github.com/Dev-folabi/denisco_backend/internal/modules/categories/infrastructure"
	inventoryinfra "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/infrastructure"
	productsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	productsinfra "github.com/Dev-folabi/denisco_backend/internal/modules/products/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/slug"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// koboPerNaira converts the prototype's naira prices into the minor units the
// API stores.
const koboPerNaira = 100

// defaultMediaBase points at the images both frontends ship in their own
// public/images folder, so the seeded catalogue renders without depending on
// an external host. Pass -media-base to use an uploaded media library instead.
const defaultMediaBase = "/images"

func main() {
	var (
		mediaBase    = flag.String("media-base", defaultMediaBase, "base path or URL for the seeded product images")
		resetStock   = flag.Bool("reset-stock", true, "reset on-hand stock to the catalogue quantities (default true: the brief requires every product in stock)")
		createOnly   = flag.Bool("create-only", false, "only create products whose slug does not exist yet; never modify, restock or archive existing ones (use on a live database)")
		archiveStale = flag.Bool("archive-stale", true, "archive active products whose slug is not in the catalogue, so a reseed replaces the range")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("failed to load configuration", err)
	}

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)

	base := strings.TrimRight(*mediaBase, "/")

	client, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		fail("failed to connect to mongodb", err)
	}
	defer func() { _ = database.Disconnect(client) }()

	db := client.Database(cfg.Mongo.Database)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.EnsureIndexes(ctx, db); err != nil {
		fail("failed to create indexes", err)
	}

	categoryRepo := categoriesinfra.NewMongoRepository(db)
	productRepo := productsinfra.NewMongoRepository(db)
	inventoryRepo := inventoryinfra.NewMongoRepository(db)

	// Categories first: products reference them by id.
	categoryIDs := make(map[string]bson.ObjectID, 5)
	for _, category := range categoriesdomain.Seed() {
		record := category
		if err := categoryRepo.Upsert(ctx, &record); err != nil {
			fail("failed to upsert category", err)
		}

		stored, err := categoryRepo.FindBySlug(ctx, record.Slug)
		if err != nil {
			fail("failed to read back category", err)
		}
		categoryIDs[stored.Slug] = stored.ID
	}
	log.Info("categories seeded", slog.Int("count", len(categoryIDs)))

	var created, updated int
	seededSlugs := make(map[string]struct{})

	for _, item := range catalogue() {
		seededSlugs[slug.Make(item.Name)] = struct{}{}
	}

	for _, item := range catalogue() {
		categoryID, ok := categoryIDs[item.Category]
		if !ok {
			fail("unknown category in seed data", errors.New(item.Category))
		}

		productSlug := slug.Make(item.Name)
		now := time.Now().UTC()

		images := []productsdomain.Image{}
		if base != "" {
			images = append(images, productsdomain.Image{URL: base + "/" + item.Image})
		}

		existing, err := productRepo.FindBySlug(ctx, productSlug)
		switch {
		case err == nil && *createOnly:
			// Existing product: leave its price, stock and images untouched.
			continue

		case err == nil:
			// Refresh the catalogue fields. The 2026-10-25 brief replaced the
			// whole range, including every image, so the seed's image now
			// overwrites whatever the record held — unlike before, when an
			// admin upload was preserved.
			fields := bson.M{
				"name":        item.Name,
				"description": item.Description,
				"category_id": categoryID,
				"price":       item.PriceNaira * koboPerNaira,
				"currency":    productsdomain.Currency,
				"unit":        item.Unit,
				"featured":    item.Featured,
				"status":      productsdomain.StatusActive,
			}
			if len(images) > 0 {
				fields["images"] = images
			}

			if err := productRepo.Update(ctx, existing.ID, fields); err != nil {
				fail("failed to update product", err)
			}
			updated++

			if err := inventoryRepo.EnsureRecord(ctx, existing.ID); err != nil {
				fail("failed to ensure stock record", err)
			}
			if *resetStock {
				if _, err := inventoryRepo.SetAvailable(ctx, existing.ID, item.Stock); err != nil {
					fail("failed to reset stock", err)
				}
			}

		case errors.Is(err, productsdomain.ErrProductNotFound):
			product := &productsdomain.Product{
				ID:          bson.NewObjectID(),
				Name:        item.Name,
				Slug:        productSlug,
				Description: item.Description,
				CategoryID:  categoryID,
				Price:       item.PriceNaira * koboPerNaira,
				Currency:    productsdomain.Currency,
				Unit:        item.Unit,
				Images:      images,
				Status:      productsdomain.StatusActive,
				Featured:    item.Featured,
				CreatedAt:   now,
				UpdatedAt:   now,
			}

			if err := productRepo.Create(ctx, product); err != nil {
				fail("failed to create product", err)
			}
			created++

			if err := inventoryRepo.EnsureRecord(ctx, product.ID); err != nil {
				fail("failed to create stock record", err)
			}
			if _, err := inventoryRepo.SetAvailable(ctx, product.ID, item.Stock); err != nil {
				fail("failed to set opening stock", err)
			}

		default:
			fail("failed to look up product", err)
		}
	}

	// Replace the range: any active product the catalogue no longer lists is
	// archived rather than deleted, so order history that names it still
	// resolves. Archiving drops it out of the shop and the public listing.
	var archived int
	if *archiveStale && !*createOnly {
		all, _, err := productRepo.List(ctx, productsdomain.Filter{Status: productsdomain.StatusActive}, 1, 1000)
		if err != nil {
			fail("failed to list products for archiving", err)
		}
		for _, product := range all {
			if _, kept := seededSlugs[product.Slug]; kept {
				continue
			}
			if err := productRepo.Update(ctx, product.ID, bson.M{"status": productsdomain.StatusArchived}); err != nil {
				fail("failed to archive stale product", err)
			}
			archived++
			log.Info("archived product not in catalogue", slog.String("slug", product.Slug))
		}
	}

	log.Info("catalogue seeded",
		slog.Int("created", created),
		slog.Int("updated", updated),
		slog.Int("archived", archived),
		slog.Bool("stock_reset", *resetStock),
		slog.String("media_base", base),
	)
}

// fail reports a fatal setup error and exits non-zero.
func fail(message string, err error) {
	slog.Error(message, slog.String("error", err.Error()))
	os.Exit(1)
}
