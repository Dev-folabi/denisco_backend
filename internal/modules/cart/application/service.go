// Package application coordinates the cart workflows and assembles the cart
// read model from the catalogue and the stock ledger.
package application

import (
	"context"

	"github.com/Dev-folabi/denisco_backend/internal/modules/cart/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// CatalogProduct is the slice of a product the cart needs. The cart depends on
// this small port rather than on the catalogue module's own types.
type CatalogProduct struct {
	ID       bson.ObjectID
	Name     string
	Slug     string
	Unit     string
	Image    string
	Price    int64
	Currency string
	Active   bool
}

// CatalogPort is the cart's view of the catalogue.
type CatalogPort interface {
	ProductsByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]CatalogProduct, error)
}

// StockPort is the cart's view of the stock ledger.
type StockPort interface {
	SellableFor(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]StockLevel, error)
}

// StockLevel is the sellable quantity and its badge.
type StockLevel struct {
	Sellable int
	Badge    string
}

// Service implements the cart workflows.
type Service struct {
	carts   domain.Repository
	catalog CatalogPort
	stock   StockPort
}

// NewService wires the cart to its repository and ports.
func NewService(carts domain.Repository, catalog CatalogPort, stock StockPort) *Service {
	return &Service{carts: carts, catalog: catalog, stock: stock}
}

// Get returns the customer's cart, priced at current catalogue prices.
func (s *Service) Get(ctx context.Context, userID bson.ObjectID) (*View, error) {
	cart, err := s.carts.Get(ctx, userID)
	if err != nil {
		return nil, apperror.From(err)
	}

	return s.buildView(ctx, cart)
}

// AddItem adds a product to the cart, or increases its quantity.
//
// Stock is checked here so the customer is told immediately, but the check is
// advisory: the authoritative reservation happens at checkout, because stock
// can change while a cart sits open.
func (s *Service) AddItem(ctx context.Context, userID, productID bson.ObjectID, quantity int) (*View, error) {
	if err := domain.ValidateQuantity(quantity); err != nil {
		return nil, err
	}

	product, level, err := s.resolve(ctx, productID)
	if err != nil {
		return nil, err
	}
	if !product.Active {
		return nil, ErrProductUnavailable
	}

	cart, err := s.carts.Get(ctx, userID)
	if err != nil {
		return nil, apperror.From(err)
	}

	// The requested quantity is on top of whatever is already in the cart.
	existing := 0
	for _, item := range cart.Items {
		if item.ProductID == productID {
			existing = item.Quantity
			break
		}
	}

	if existing+quantity > level.Sellable {
		return nil, ErrInsufficientStock.WithDetails(map[string]any{
			"product_id": productID.Hex(),
			"available":  level.Sellable,
			"requested":  existing + quantity,
		})
	}

	if err := s.carts.AddItem(ctx, userID, productID, quantity); err != nil {
		return nil, apperror.From(err)
	}

	return s.Get(ctx, userID)
}

// SetQuantity replaces a line's quantity.
func (s *Service) SetQuantity(ctx context.Context, userID, productID bson.ObjectID, quantity int) (*View, error) {
	if err := domain.ValidateQuantity(quantity); err != nil {
		return nil, err
	}

	_, level, err := s.resolve(ctx, productID)
	if err != nil {
		return nil, err
	}

	if quantity > level.Sellable {
		return nil, ErrInsufficientStock.WithDetails(map[string]any{
			"product_id": productID.Hex(),
			"available":  level.Sellable,
			"requested":  quantity,
		})
	}

	updated, err := s.carts.SetQuantity(ctx, userID, productID, quantity)
	if err != nil {
		return nil, apperror.From(err)
	}
	if !updated {
		return nil, domain.ErrItemNotInCart
	}

	return s.Get(ctx, userID)
}

// RemoveItem drops a line from the cart.
func (s *Service) RemoveItem(ctx context.Context, userID, productID bson.ObjectID) (*View, error) {
	removed, err := s.carts.RemoveItem(ctx, userID, productID)
	if err != nil {
		return nil, apperror.From(err)
	}
	if !removed {
		return nil, domain.ErrItemNotInCart
	}

	return s.Get(ctx, userID)
}

// Clear empties the cart.
func (s *Service) Clear(ctx context.Context, userID bson.ObjectID) (*View, error) {
	if err := s.carts.Clear(ctx, userID); err != nil {
		return nil, apperror.From(err)
	}
	return s.Get(ctx, userID)
}

// resolve loads one product and its stock, for the add and update paths.
func (s *Service) resolve(ctx context.Context, productID bson.ObjectID) (CatalogProduct, StockLevel, error) {
	products, err := s.catalog.ProductsByIDs(ctx, []bson.ObjectID{productID})
	if err != nil {
		return CatalogProduct{}, StockLevel{}, apperror.From(err)
	}

	product, ok := products[productID]
	if !ok {
		return CatalogProduct{}, StockLevel{}, ErrProductNotFound
	}

	levels, err := s.stock.SellableFor(ctx, []bson.ObjectID{productID})
	if err != nil {
		return CatalogProduct{}, StockLevel{}, apperror.From(err)
	}

	return product, levels[productID], nil
}

// buildView prices the cart from the catalogue and reports per-line stock.
//
// A line whose product has been archived or deleted is kept visible and marked
// unavailable rather than silently dropped, so the customer understands why
// their total changed.
func (s *Service) buildView(ctx context.Context, cart *domain.Cart) (*View, error) {
	view := &View{Items: []ItemView{}, Currency: "NGN"}
	if len(cart.Items) == 0 {
		return view, nil
	}

	ids := cart.ProductIDs()

	products, err := s.catalog.ProductsByIDs(ctx, ids)
	if err != nil {
		return nil, apperror.From(err)
	}

	levels, err := s.stock.SellableFor(ctx, ids)
	if err != nil {
		return nil, apperror.From(err)
	}

	for _, item := range cart.Items {
		product, known := products[item.ProductID]
		level := levels[item.ProductID]

		line := ItemView{
			ID:         item.ProductID.Hex(),
			ProductID:  item.ProductID.Hex(),
			Quantity:   item.Quantity,
			Stock:      level.Sellable,
			StockBadge: level.Badge,
			AddedAt:    item.AddedAt,
		}

		if known {
			line.Name = product.Name
			line.Slug = product.Slug
			line.Unit = product.Unit
			line.Image = product.Image
			line.UnitPrice = product.Price
			line.LineTotal = product.Price * int64(item.Quantity)
			line.Available = product.Active && level.Sellable >= item.Quantity
		} else {
			line.Name = "Unavailable product"
			line.Available = false
		}

		view.Items = append(view.Items, line)
		view.Subtotal += line.LineTotal
		view.ItemCount += item.Quantity

		if !line.Available {
			view.HasUnavailableItems = true
		}
	}

	return view, nil
}
