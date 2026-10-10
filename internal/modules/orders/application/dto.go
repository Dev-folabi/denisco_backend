// Package application coordinates the checkout and order-lifecycle workflows.
package application

import (
	"context"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// CatalogProduct is the slice of a product the checkout needs.
type CatalogProduct struct {
	ID     bson.ObjectID
	Name   string
	Slug   string
	Unit   string
	Image  string
	Price  int64
	Active bool
}

// CatalogPort is the order module's view of the catalogue.
type CatalogPort interface {
	ProductsByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]CatalogProduct, error)
}

// CartPort is the order module's view of the cart.
type CartPort interface {
	// Lines returns the customer's cart as product/quantity pairs.
	Lines(ctx context.Context, userID bson.ObjectID) ([]CartLine, error)
	// Clear empties the cart once its contents have become an order.
	Clear(ctx context.Context, userID bson.ObjectID) error
}

// CartLine is one product and quantity from the cart.
type CartLine struct {
	ProductID bson.ObjectID
	Quantity  int
}

// StockPort is the order module's view of the stock ledger.
type StockPort interface {
	// Reserve promises stock to an order, failing when it is unavailable.
	Reserve(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error
	// Release returns a reservation to the sellable pool.
	Release(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error
	// Deduct removes paid-for stock.
	Deduct(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error
	// SellableFor returns how many units of each product may still be sold,
	// used to decide whether a late payment can be honoured.
	SellableFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]int, error)
	// Sell takes stock for an order that holds no reservation, which is the
	// recovery step for a payment that arrived after its reservation expired.
	Sell(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error
}

// TxRunner runs a unit of work inside a database transaction.
type TxRunner interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}

// CheckoutInput is a request to turn the cart into an order.
type CheckoutInput struct {
	Name           string
	Email          string
	Phone          string
	DeliveryMethod string
	Address        string
}

// ItemView is one line on an order.
type ItemView struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	Image     string `json:"image,omitempty"`
	Unit      string `json:"unit,omitempty"`
	UnitPrice int64  `json:"unit_price"`
	Quantity  int    `json:"quantity"`
	LineTotal int64  `json:"line_total"`
}

// CustomerView is the contact detail captured at checkout.
type CustomerView struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

// View is the public projection of an order.
type View struct {
	ID             string       `json:"id"`
	OrderNumber    string       `json:"order_number"`
	UserID         string       `json:"user_id"`
	Customer       CustomerView `json:"customer"`
	Items          []ItemView   `json:"items"`
	Subtotal       int64        `json:"subtotal"`
	DeliveryFee    int64        `json:"delivery_fee"`
	Total          int64        `json:"total"`
	Currency       string       `json:"currency"`
	DeliveryMethod string       `json:"delivery_method"`
	Address        string       `json:"address,omitempty"`
	Status         string       `json:"status"`
	PaymentStatus  string       `json:"payment_status"`
	PaymentRef     string       `json:"payment_ref,omitempty"`
	PaymentMethod  string       `json:"payment_method,omitempty"`
	ItemCount      int          `json:"item_count"`
	// NeedsReconciliation flags an order whose late payment could not be
	// honoured from stock, so the dashboard can surface it for a refund.
	NeedsReconciliation  bool    `json:"needs_reconciliation"`
	ReconciliationReason string  `json:"reconciliation_reason,omitempty"`
	PaidAt               *string `json:"paid_at,omitempty"`
	CancelledAt          *string `json:"cancelled_at,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

// newView projects an order into its API representation.
func newView(order *domain.Order) View {
	items := make([]ItemView, 0, len(order.Items))
	count := 0

	for _, item := range order.Items {
		items = append(items, ItemView{
			ProductID: item.ProductID.Hex(),
			Name:      item.Name,
			Slug:      item.Slug,
			Image:     item.Image,
			Unit:      item.Unit,
			UnitPrice: item.UnitPrice,
			Quantity:  item.Quantity,
			LineTotal: item.LineTotal,
		})
		count += item.Quantity
	}

	view := View{
		ID:          order.ID.Hex(),
		OrderNumber: order.OrderNumber,
		UserID:      order.UserID.Hex(),
		Customer: CustomerView{
			Name:  order.Customer.Name,
			Email: order.Customer.Email,
			Phone: order.Customer.Phone,
		},
		Items:          items,
		Subtotal:       order.Subtotal,
		DeliveryFee:    order.DeliveryFee,
		Total:          order.Total,
		Currency:       order.Currency,
		DeliveryMethod: order.DeliveryMethod,
		Address:        order.Address,
		Status:         order.Status,
		PaymentStatus:  order.PaymentStatus,
		PaymentRef:     order.PaymentRef,
		PaymentMethod:  order.PaymentMethod,
		ItemCount:      count,

		NeedsReconciliation:  order.NeedsReconciliation,
		ReconciliationReason: order.ReconciliationReason,
		CreatedAt:            order.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            order.UpdatedAt.UTC().Format(time.RFC3339),
	}

	if order.PaidAt != nil {
		paidAt := order.PaidAt.UTC().Format(time.RFC3339)
		view.PaidAt = &paidAt
	}
	if order.CancelledAt != nil {
		cancelledAt := order.CancelledAt.UTC().Format(time.RFC3339)
		view.CancelledAt = &cancelledAt
	}

	return view
}

// Actor identifies the administrator behind an order transition.
type Actor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}
