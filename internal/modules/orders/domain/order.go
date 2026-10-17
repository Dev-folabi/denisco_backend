// Package domain holds the order entity, its state machine and the pricing
// rules taken from the prototype.
package domain

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Fulfillment statuses, matching the admin dashboard's status dropdown in
// denisco_prototype.html.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDispatched = "dispatched"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
)

// Payment statuses tracked on the order. The payments module owns the finer
// provider-level states; this is the order's own view of them.
const (
	PaymentPending  = "pending"
	PaymentPaid     = "paid"
	PaymentFailed   = "failed"
	PaymentRefunded = "refunded"
)

// Reconciliation reasons recorded on an order whose payment arrived after its
// stock reservation had already been returned to the shop. The architecture
// requires a late payment to enter an explicit reconciliation path rather than
// silently recreating a reservation.
const (
	// ReconciliationStockSecured means the units were still on the shelf and
	// were taken for the order, which can therefore be fulfilled.
	ReconciliationStockSecured = "stock_secured"
	// ReconciliationRefundRequired means the units were gone by the time the
	// payment landed, so an administrator must refund.
	ReconciliationRefundRequired = "refund_required"
)

// Delivery methods offered at checkout.
const (
	DeliveryHome   = "delivery"
	DeliveryPickup = "pickup"
)

// Currency is fixed to naira; amounts are stored in kobo.
const Currency = "NGN"

// Order errors.
var (
	ErrOrderNotFound     = apperror.New(http.StatusNotFound, "ORDER_NOT_FOUND", "The order could not be found.")
	ErrNotCancellable    = apperror.New(http.StatusConflict, "ORDER_NOT_CANCELLABLE", "This order can no longer be cancelled.")
	ErrInvalidStatus     = apperror.New(http.StatusUnprocessableEntity, "INVALID_ORDER_STATUS", "That is not a valid order status.")
	ErrInvalidTransition = apperror.New(http.StatusConflict, "INVALID_STATUS_TRANSITION", "The order cannot move to that status from its current one.")
	ErrAddressRequired   = apperror.New(http.StatusUnprocessableEntity, "DELIVERY_ADDRESS_REQUIRED", "A delivery address is required for home delivery.")
	ErrNotPayable        = apperror.New(http.StatusConflict, "ORDER_NOT_PAYABLE", "This order is not awaiting payment.")
)

// Customer is the contact detail captured at checkout.
//
// It is stored on the order rather than read from the account, because the
// customer may order on someone else's behalf and because the order must
// still make sense if the profile later changes.
type Customer struct {
	Name  string `bson:"name"`
	Email string `bson:"email"`
	Phone string `bson:"phone"`
}

// Item is a line on an order: a snapshot of the product as it was sold, so a
// later price or name change never rewrites history.
type Item struct {
	ProductID bson.ObjectID `bson:"product_id"`
	Name      string        `bson:"name"`
	Slug      string        `bson:"slug,omitempty"`
	Image     string        `bson:"image,omitempty"`
	Unit      string        `bson:"unit,omitempty"`
	UnitPrice int64         `bson:"unit_price"`
	Quantity  int           `bson:"quantity"`
	LineTotal int64         `bson:"line_total"`
}

// Order is a customer's placed order.
type Order struct {
	ID             bson.ObjectID `bson:"_id,omitempty"`
	OrderNumber    string        `bson:"order_number"`
	UserID         bson.ObjectID `bson:"user_id"`
	Customer       Customer      `bson:"customer"`
	Items          []Item        `bson:"items"`
	Subtotal       int64         `bson:"subtotal"`
	DeliveryFee    int64         `bson:"delivery_fee"`
	Total          int64         `bson:"total"`
	Currency       string        `bson:"currency"`
	DeliveryMethod string        `bson:"delivery_method"`
	Address        string        `bson:"address,omitempty"`
	// DeliveryLocationID and DeliveryLocationName snapshot the area an order
	// was sent to, so a later rename or removal does not rewrite history.
	// Both are empty for farm pickup and for orders placed before areas
	// existed, which were charged a flat fee.
	DeliveryLocationID   bson.ObjectID `bson:"delivery_location_id,omitempty"`
	DeliveryLocationName string        `bson:"delivery_location_name,omitempty"`
	Status               string        `bson:"status"`
	PaymentStatus        string        `bson:"payment_status"`
	PaymentRef           string        `bson:"payment_ref,omitempty"`
	PaymentMethod        string        `bson:"payment_method,omitempty"`
	// StockReserved records whether the order still holds a stock
	// reservation, so release and deduction happen exactly once.
	StockReserved bool `bson:"stock_reserved"`
	// NeedsReconciliation marks an order whose payment arrived after its
	// reservation expired and could not be re-stocked, so money is held
	// against goods that cannot be shipped.
	NeedsReconciliation  bool       `bson:"needs_reconciliation,omitempty"`
	ReconciliationReason string     `bson:"reconciliation_reason,omitempty"`
	PaidAt               *time.Time `bson:"paid_at,omitempty"`
	CancelledAt          *time.Time `bson:"cancelled_at,omitempty"`
	CreatedAt            time.Time  `bson:"created_at"`
	UpdatedAt            time.Time  `bson:"updated_at"`
}

// IsPaid reports whether payment has been confirmed.
func (o *Order) IsPaid() bool {
	return o.PaymentStatus == PaymentPaid
}

// IsCancellable reports whether a customer may still cancel. Once money has
// changed hands, cancellation becomes a refund decision for an administrator.
func (o *Order) IsCancellable() bool {
	return o.Status == StatusPending && o.PaymentStatus == PaymentPending
}

// IsPayable reports whether a payment attempt may be started for this order.
//
// A failed attempt does not close the order: the architecture requires each
// attempt to be its own record, which only makes sense if the customer whose
// card was declined can try again. What closes it is money having changed
// hands — paid or refunded.
func (o *Order) IsPayable() bool {
	if o.Status == StatusCancelled {
		return false
	}
	return o.PaymentStatus == PaymentPending || o.PaymentStatus == PaymentFailed
}

// ProductIDs lists the products on the order.
func (o *Order) ProductIDs() []bson.ObjectID {
	ids := make([]bson.ObjectID, 0, len(o.Items))
	for _, item := range o.Items {
		ids = append(ids, item.ProductID)
	}
	return ids
}

// allowedTransitions is the fulfillment state machine. An order moves forward
// through handling, and may be cancelled until it has been dispatched.
var allowedTransitions = map[string][]string{
	StatusPending:    {StatusProcessing, StatusCancelled},
	StatusProcessing: {StatusDispatched, StatusCancelled},
	StatusDispatched: {StatusCompleted},
	StatusCompleted:  {},
	StatusCancelled:  {},
}

// ValidateStatus checks a fulfillment status name.
func ValidateStatus(status string) error {
	if _, ok := allowedTransitions[status]; !ok {
		return ErrInvalidStatus
	}
	return nil
}

// CanTransition reports whether an order may move between two statuses.
func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// ValidateDeliveryMethod checks the chosen fulfilment option.
func ValidateDeliveryMethod(method string) error {
	switch method {
	case DeliveryHome, DeliveryPickup:
		return nil
	default:
		return apperror.Validation.WithDetails(map[string]any{
			"delivery_method": "must be \"delivery\" or \"pickup\"",
		})
	}
}

// ValidateAddress requires an address for home delivery and ignores it for
// farm pickup.
func ValidateAddress(method, address string) (string, error) {
	trimmed := strings.TrimSpace(address)

	if method == DeliveryPickup {
		return "", nil
	}
	if trimmed == "" {
		return "", ErrAddressRequired
	}
	if len(trimmed) > 500 {
		return "", apperror.Validation.WithDetails(map[string]any{"address": "must be 500 characters or fewer"})
	}

	return trimmed, nil
}

// Repository is the persistence contract for orders.
type Repository interface {
	// Create inserts an order.
	Create(ctx context.Context, order *Order) error

	// FindByID returns an order or ErrOrderNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Order, error)

	// FindByNumber returns an order by its customer-facing number.
	FindByNumber(ctx context.Context, number string) (*Order, error)

	// FindByPaymentRef returns the order a payment reference belongs to.
	FindByPaymentRef(ctx context.Context, reference string) (*Order, error)

	// ListForUser returns a customer's orders, newest first.
	ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]Order, int64, error)

	// List returns orders for the admin dashboard, filtered and paginated.
	List(ctx context.Context, filter Filter, page, limit int) ([]Order, int64, error)

	// Update applies a set of changed fields.
	Update(ctx context.Context, id bson.ObjectID, fields bson.M) error

	// MarkPaid flips an order to paid only while no payment has settled —
	// pending, or failed and retried — reporting whether this call was the
	// one that applied the change. That is what makes repeated payment
	// webhooks idempotent.
	MarkPaid(ctx context.Context, id bson.ObjectID, reference string, paidAt time.Time) (bool, error)

	// ReleaseReservation clears the reservation flag only while it is set,
	// so stock is released exactly once.
	ReleaseReservation(ctx context.Context, id bson.ObjectID) (bool, error)

	// CountForProduct reports how many orders contain a product, so the
	// catalogue can tell a product with history from one without.
	CountForProduct(ctx context.Context, productID bson.ObjectID) (int64, error)

	// NumberExists reports whether an order number is already taken.
	NumberExists(ctx context.Context, number string) (bool, error)

	// Stats returns aggregate counts and revenue for the admin dashboard.
	Stats(ctx context.Context) (Stats, error)

	// Recent returns the newest orders, for the admin dashboard.
	Recent(ctx context.Context, limit int) ([]Order, error)

	// StaleReserved returns orders still holding stock that were never paid
	// for and are older than the cut-off.
	StaleReserved(ctx context.Context, before time.Time, limit int) ([]Order, error)

	// DailyRevenue returns paid revenue per day since a cut-off, for the
	// dashboard's sales chart.
	DailyRevenue(ctx context.Context, since time.Time) (map[string]int64, error)
}

// DailyRevenuePoint is one bar on the dashboard's sales chart.
type DailyRevenuePoint struct {
	Date    string
	Revenue int64
}

// Filter narrows an administrative order listing.
type Filter struct {
	UserID        *bson.ObjectID
	Status        string
	PaymentStatus string
	// NeedsReconciliation, when set, narrows the list to orders awaiting (or
	// clear of) a late-payment decision.
	NeedsReconciliation *bool
	// Search matches the order number or the customer's name and email.
	Search string
}

// Stats is the order summary shown on the admin dashboard.
type Stats struct {
	TotalOrders   int64
	PendingOrders int64
	PaidOrders    int64
	Revenue       int64
}
