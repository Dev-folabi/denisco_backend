package bootstrap

import (
	"context"
	"time"

	cartapp "github.com/Dev-folabi/denisco_backend/internal/modules/cart/application"
	cartdomain "github.com/Dev-folabi/denisco_backend/internal/modules/cart/domain"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	deliveryapp "github.com/Dev-folabi/denisco_backend/internal/modules/delivery/application"
	inventoryapp "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/application"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	paymentsapp "github.com/Dev-folabi/denisco_backend/internal/modules/payments/application"
	productsapp "github.com/Dev-folabi/denisco_backend/internal/modules/products/application"
	productsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// catalogueStock adapts the inventory service to the small port the catalogue
// declares. The adapter lives here, in the wiring, so neither module has to
// import the other.
type catalogueStock struct {
	inventory *inventoryapp.Service
}

// LevelsFor returns stock for a set of products.
func (c catalogueStock) LevelsFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]productsapp.Stock, error) {
	levels, err := c.inventory.LevelsFor(ctx, productIDs)
	if err != nil {
		return nil, err
	}

	stock := make(map[bson.ObjectID]productsapp.Stock, len(levels))
	for id, level := range levels {
		stock[id] = productsapp.Stock{
			Available: level.Available,
			Reserved:  level.Reserved,
			Sellable:  level.Sellable,
			Badge:     level.Badge,
		}
	}

	return stock, nil
}

// EnsureRecord opens the stock record for a new product.
func (c catalogueStock) EnsureRecord(ctx context.Context, productID bson.ObjectID) error {
	return c.inventory.EnsureRecord(ctx, productID)
}

// SetStock replaces a product's on-hand quantity.
func (c catalogueStock) SetStock(ctx context.Context, productID bson.ObjectID, quantity int, actor productsapp.StockActor) error {
	return c.inventory.SetStock(ctx, productID, quantity, &inventoryapp.Actor{
		ID:    actor.ID,
		Email: actor.Email,
		Role:  actor.Role,
	})
}

// PurgeRecord forgets a deleted product's stock entirely.
func (c catalogueStock) PurgeRecord(ctx context.Context, productID bson.ObjectID) error {
	return c.inventory.PurgeProduct(ctx, productID)
}

// catalogueSales answers the one question the catalogue has about orders:
// whether a product has ever been bought.
//
// It reads the order repository rather than the order service, because the
// order service is itself built on the catalogue — the question is a plain
// lookup, and routing it through the service would make the wiring circular.
type catalogueSales struct {
	orders ordersdomain.Repository
}

// HasOrdersFor reports whether any order contains the product.
func (c catalogueSales) HasOrdersFor(ctx context.Context, productID bson.ObjectID) (bool, error) {
	count, err := c.orders.CountForProduct(ctx, productID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// cartCatalog adapts the catalogue to the port the cart declares.
type cartCatalog struct {
	products *productsapp.Service
}

// ProductsByIDs returns the catalogue fields the cart needs.
func (c cartCatalog) ProductsByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]cartapp.CatalogProduct, error) {
	views, err := c.products.Lookup(ctx, ids)
	if err != nil {
		return nil, err
	}

	products := make(map[bson.ObjectID]cartapp.CatalogProduct, len(views))
	for id, view := range views {
		products[id] = cartapp.CatalogProduct{
			ID:       id,
			Name:     view.Name,
			Slug:     view.Slug,
			Unit:     view.Unit,
			Image:    view.PrimaryImage(),
			Price:    view.Price,
			Currency: view.Currency,
			Active:   view.Status == productsdomain.StatusActive,
		}
	}

	return products, nil
}

// cartStock adapts the inventory service to the port the cart declares.
type cartStock struct {
	inventory *inventoryapp.Service
}

// SellableFor returns sellable quantities for a set of products.
func (c cartStock) SellableFor(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]cartapp.StockLevel, error) {
	levels, err := c.inventory.LevelsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	stock := make(map[bson.ObjectID]cartapp.StockLevel, len(levels))
	for id, level := range levels {
		stock[id] = cartapp.StockLevel{Sellable: level.Sellable, Badge: level.Badge}
	}

	return stock, nil
}

// orderCatalog adapts the catalogue to the port the order module declares.
type orderCatalog struct {
	products *productsapp.Service
}

// ProductsByIDs returns the catalogue fields checkout needs to price an order.
func (o orderCatalog) ProductsByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]ordersapp.CatalogProduct, error) {
	views, err := o.products.Lookup(ctx, ids)
	if err != nil {
		return nil, err
	}

	products := make(map[bson.ObjectID]ordersapp.CatalogProduct, len(views))
	for id, view := range views {
		products[id] = ordersapp.CatalogProduct{
			ID:     id,
			Name:   view.Name,
			Slug:   view.Slug,
			Unit:   view.Unit,
			Image:  view.PrimaryImage(),
			Price:  view.Price,
			Active: view.Status == productsdomain.StatusActive,
		}
	}

	return products, nil
}

// orderCart adapts the cart to the port the order module declares.
type orderCart struct {
	carts cartdomain.Repository
}

// Lines returns the customer's cart as product/quantity pairs.
func (o orderCart) Lines(ctx context.Context, userID bson.ObjectID) ([]ordersapp.CartLine, error) {
	cart, err := o.carts.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	lines := make([]ordersapp.CartLine, 0, len(cart.Items))
	for _, item := range cart.Items {
		lines = append(lines, ordersapp.CartLine{ProductID: item.ProductID, Quantity: item.Quantity})
	}

	return lines, nil
}

// Clear empties the cart once its contents have become an order.
func (o orderCart) Clear(ctx context.Context, userID bson.ObjectID) error {
	return o.carts.Clear(ctx, userID)
}

// orderDelivery adapts the delivery service to the port the order module
// declares.
type orderDelivery struct {
	delivery *deliveryapp.Service
}

// ActiveLocation returns a sellable delivery area, or false when it cannot
// be delivered to — unknown, retired, or the offering switched off.
func (o orderDelivery) ActiveLocation(ctx context.Context, id bson.ObjectID) (ordersapp.DeliveryLocation, bool, error) {
	location, ok, err := o.delivery.CheckoutLocation(ctx, id)
	if err != nil || !ok {
		return ordersapp.DeliveryLocation{}, false, err
	}

	return ordersapp.DeliveryLocation{
		ID:   location.ID,
		Name: location.Name,
		Fee:  location.Fee,
	}, true, nil
}

// orderStock adapts the inventory service to the port the order module
// declares.
type orderStock struct {
	inventory *inventoryapp.Service
}

// Reserve promises stock to an order.
func (o orderStock) Reserve(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error {
	return o.inventory.Reserve(ctx, productID, quantity, reference)
}

// Release returns a reservation to the sellable pool.
func (o orderStock) Release(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error {
	return o.inventory.Release(ctx, productID, quantity, reference, reason)
}

// Deduct removes paid-for stock.
func (o orderStock) Deduct(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error {
	return o.inventory.Deduct(ctx, productID, quantity, reference)
}

// SellableFor reports what may still be sold, for the late-payment decision.
func (o orderStock) SellableFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]int, error) {
	return o.inventory.SellableFor(ctx, productIDs)
}

// Sell takes stock for an order that holds no reservation.
func (o orderStock) Sell(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error {
	return o.inventory.Sell(ctx, productID, quantity, reference, reason)
}

// paymentOrders adapts the order service to the port the payments module
// declares.
type paymentOrders struct {
	orders *ordersapp.Service
}

// ForPayment loads an order that is still awaiting payment.
func (p paymentOrders) ForPayment(ctx context.Context, orderID string, userID bson.ObjectID, isAdmin bool) (*ordersdomain.Order, error) {
	return p.orders.OrderForPayment(ctx, orderID, ordersapp.Viewer{UserID: userID, IsAdmin: isAdmin})
}

// Load returns an order regardless of its payment state.
func (p paymentOrders) Load(ctx context.Context, orderID bson.ObjectID) (*ordersdomain.Order, error) {
	view, err := p.orders.Get(ctx, orderID.Hex(), ordersapp.Viewer{IsAdmin: true})
	if err != nil {
		return nil, err
	}
	// Only the identifier is needed by the callers of this method today.
	return &ordersdomain.Order{ID: orderID, OrderNumber: view.OrderNumber}, nil
}

// MarkPaid records a confirmed payment and deducts the reserved stock.
func (p paymentOrders) MarkPaid(ctx context.Context, orderID bson.ObjectID, reference, method string, paidAt time.Time) error {
	return p.orders.MarkPaid(ctx, orderID, reference, method, paidAt)
}

// MarkPaymentFailed records a failed attempt and releases the stock.
func (p paymentOrders) MarkPaymentFailed(ctx context.Context, orderID bson.ObjectID, status string) error {
	return p.orders.MarkPaymentFailed(ctx, orderID, status)
}

// MarkRefunded records a refund against the order.
func (p paymentOrders) MarkRefunded(ctx context.Context, orderID bson.ObjectID) error {
	return p.orders.MarkRefunded(ctx, orderID)
}

// paymentBookings adapts the consultation service to the port the payments
// module declares, translating between the two modules' own types.
type paymentBookings struct {
	consultations *consultapp.Service
}

// ForPayment loads a booking whose consultation fee is still outstanding.
func (p paymentBookings) ForPayment(ctx context.Context, bookingID, bookingReference string, userID bson.ObjectID, isAdmin bool) (*paymentsapp.BookingTarget, error) {
	booking, err := p.consultations.BookingForPayment(ctx, bookingID, bookingReference,
		consultapp.Viewer{UserID: userID, IsAdmin: isAdmin})
	if err != nil {
		return nil, err
	}

	target := &paymentsapp.BookingTarget{
		ID:        booking.ID,
		Reference: booking.Reference,
		TypeName:  booking.TypeName,
		Amount:    booking.TypePrice,
		Currency:  consultdomain.Currency,
		Name:      booking.Name,
		Email:     booking.Email,
	}
	if booking.UserID != nil {
		target.UserID = *booking.UserID
	}

	return target, nil
}

// MarkPaid settles the fee and confirms the booking.
func (p paymentBookings) MarkPaid(ctx context.Context, bookingID bson.ObjectID, reference string, paidAt time.Time) error {
	return p.consultations.MarkBookingPaid(ctx, bookingID, reference, paidAt)
}

// MarkPaymentFailed records a declined or abandoned attempt.
func (p paymentBookings) MarkPaymentFailed(ctx context.Context, bookingID bson.ObjectID, status string) error {
	return p.consultations.MarkBookingPaymentFailed(ctx, bookingID, status)
}

// MarkRefunded records a refunded fee.
func (p paymentBookings) MarkRefunded(ctx context.Context, bookingID bson.ObjectID) error {
	return p.consultations.MarkBookingRefunded(ctx, bookingID)
}
