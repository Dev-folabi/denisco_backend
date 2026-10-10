// Package infrastructure holds the MongoDB implementation of the product
// repository.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository persists products in the `products` collection.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository wires the repository to a database handle.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection(database.CollProducts)}
}

// Create inserts a product, translating the unique-slug violation into the
// domain error.
func (r *MongoRepository) Create(ctx context.Context, product *domain.Product) error {
	if product.ID.IsZero() {
		product.ID = bson.NewObjectID()
	}

	if _, err := r.collection.InsertOne(ctx, product); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrSlugTaken
		}
		return fmt.Errorf("products: insert: %w", err)
	}

	return nil
}

// Update applies changed fields and stamps updated_at.
func (r *MongoRepository) Update(ctx context.Context, id bson.ObjectID, fields bson.M) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()

	result, err := r.collection.UpdateByID(ctx, id, bson.M{"$set": fields})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrSlugTaken
		}
		return fmt.Errorf("products: update: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrProductNotFound
	}

	return nil
}

// FindByID loads a product by identifier.
func (r *MongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Product, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// FindBySlug loads a product by slug.
func (r *MongoRepository) FindBySlug(ctx context.Context, slug string) (*domain.Product, error) {
	return r.findOne(ctx, bson.M{"slug": slug})
}

// findOne is the shared lookup, mapping "no documents" to the domain error.
func (r *MongoRepository) findOne(ctx context.Context, filter bson.M) (*domain.Product, error) {
	var product domain.Product
	if err := r.collection.FindOne(ctx, filter).Decode(&product); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrProductNotFound
		}
		return nil, fmt.Errorf("products: find: %w", err)
	}
	return &product, nil
}

// FindByIDs loads several products in one query, used to build cart and order
// views without an N+1.
func (r *MongoRepository) FindByIDs(ctx context.Context, ids []bson.ObjectID) ([]domain.Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	cursor, err := r.collection.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("products: find by ids: %w", err)
	}
	defer cursor.Close(ctx)

	products := make([]domain.Product, 0, len(ids))
	if err := cursor.All(ctx, &products); err != nil {
		return nil, fmt.Errorf("products: decode: %w", err)
	}

	return products, nil
}

// List returns a filtered, sorted page with the total match count.
func (r *MongoRepository) List(ctx context.Context, filter domain.Filter, page, limit int) ([]domain.Product, int64, error) {
	query := buildQuery(filter)

	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("products: count: %w", err)
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().
			SetSort(buildSort(filter.Sort)).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("products: list: %w", err)
	}
	defer cursor.Close(ctx)

	products := make([]domain.Product, 0, limit)
	if err := cursor.All(ctx, &products); err != nil {
		return nil, 0, fmt.Errorf("products: decode: %w", err)
	}

	return products, total, nil
}

// SlugExists reports whether a slug is taken.
func (r *MongoRepository) SlugExists(ctx context.Context, slug string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"slug": slug}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("products: slug exists: %w", err)
	}
	return count > 0, nil
}

// Count returns the number of products with a status, or all when empty.
func (r *MongoRepository) Count(ctx context.Context, status string) (int64, error) {
	query := bson.M{}
	if status != "" {
		query["status"] = status
	}

	count, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("products: count: %w", err)
	}
	return count, nil
}

// Delete removes a product document.
func (r *MongoRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("products: delete: %w", err)
	}
	if result.DeletedCount == 0 {
		return domain.ErrProductNotFound
	}
	return nil
}

// buildQuery turns the domain filter into a MongoDB query.
func buildQuery(filter domain.Filter) bson.M {
	query := bson.M{}

	if filter.CategoryID != nil {
		query["category_id"] = *filter.CategoryID
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.Featured != nil {
		query["featured"] = *filter.Featured
	}
	if filter.Search != "" {
		// A case-insensitive prefix-or-substring match on name and
		// description. The text index exists for larger catalogues; this keeps
		// partial-word search (as the shop's search box expects) working.
		pattern := bson.M{"$regex": regexpQuote(filter.Search), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"name": pattern},
			bson.M{"description": pattern},
		}
	}

	return query
}

// buildSort maps a sort key to a MongoDB sort document.
func buildSort(sort string) bson.D {
	switch sort {
	case domain.SortPriceAsc:
		return bson.D{{Key: "price", Value: 1}, {Key: "name", Value: 1}}
	case domain.SortPriceDesc:
		return bson.D{{Key: "price", Value: -1}, {Key: "name", Value: 1}}
	case domain.SortName:
		return bson.D{{Key: "name", Value: 1}}
	case domain.SortNewest:
		return bson.D{{Key: "created_at", Value: -1}}
	default:
		// "Featured" puts flagged products first, then the catalogue order.
		return bson.D{{Key: "featured", Value: -1}, {Key: "created_at", Value: 1}}
	}
}
