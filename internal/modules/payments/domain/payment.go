// Package domain holds the payment-attempt entity and its state machine.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Payment-attempt statuses, as listed in the root implementation plan.
const (
	StatusInitialized = "initialized"
	StatusPending     = "pending"
	StatusSuccessful  = "successful"
	StatusFailed      = "failed"
	StatusAbandoned   = "abandoned"
	StatusExpired     = "expired"
	StatusRefunded    = "refunded"
)

// ProviderPaystack is the only provider implemented.
const ProviderPaystack = "paystack"

// What an attempt is paying for. An empty purpose on a stored record means
// PurposeOrder, which is all there was before consultations became payable.
const (
	PurposeOrder        = "order"
	PurposeConsultation = "consultation"
)

// Payment errors.
var (
	ErrPaymentNotFound = apperror.New(http.StatusNotFound, "PAYMENT_NOT_FOUND", "The payment could not be found.")
	ErrAmountMismatch  = apperror.New(http.StatusConflict, "PAYMENT_AMOUNT_MISMATCH", "The payment amount does not match the amount due.")
	ErrNotRefundable   = apperror.New(http.StatusConflict, "PAYMENT_NOT_REFUNDABLE", "Only a successful payment can be refunded.")
)

// Payment is one attempt to pay for an order or a consultation booking.
//
// Each attempt is its own record with its own reference: a customer who
// retries after a failure gets a second record, and the failed one is kept
// rather than overwritten, so the history stays auditable.
//
// Exactly one of the order or booking fields is set, chosen by Purpose. They
// are separate fields rather than one polymorphic identifier so the admin
// transaction list can read either without a join, and so a query for an
// order's attempts cannot accidentally match a booking's.
type Payment struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Reference string        `bson:"reference"`
	// Purpose is "order" or "consultation"; empty on records written before
	// consultations became payable, which are all orders.
	Purpose string        `bson:"purpose,omitempty"`
	OrderID bson.ObjectID `bson:"order_id,omitempty"`
	// OrderNumber is denormalised so the admin transaction list needs no join.
	OrderNumber string `bson:"order_number,omitempty"`
	// BookingID and BookingReference are the consultation equivalents.
	BookingID        bson.ObjectID `bson:"booking_id,omitempty"`
	BookingReference string        `bson:"booking_reference,omitempty"`
	// UserID is zero for a guest consultation payment: the booking page is
	// public, so a visitor can pay without an account.
	UserID       bson.ObjectID `bson:"user_id,omitempty"`
	CustomerName string        `bson:"customer_name,omitempty"`
	Email        string        `bson:"email,omitempty"`
	Amount       int64         `bson:"amount"`
	Currency     string        `bson:"currency"`
	Status       string        `bson:"status"`
	Provider     string        `bson:"provider"`
	// ProviderReference is Paystack's own identifier for the transaction.
	ProviderReference string     `bson:"provider_reference,omitempty"`
	Channel           string     `bson:"channel,omitempty"`
	AuthorizationURL  string     `bson:"authorization_url,omitempty"`
	GatewayResponse   string     `bson:"gateway_response,omitempty"`
	PaidAt            *time.Time `bson:"paid_at,omitempty"`
	RefundedAt        *time.Time `bson:"refunded_at,omitempty"`
	CreatedAt         time.Time  `bson:"created_at"`
	UpdatedAt         time.Time  `bson:"updated_at"`
}

// Kind returns the attempt's purpose, defaulting a record written before
// consultations became payable to an order.
func (p *Payment) Kind() string {
	if p.Purpose == PurposeConsultation {
		return PurposeConsultation
	}
	return PurposeOrder
}

// IsGuest reports whether the attempt belongs to no account, which only a
// consultation payment can.
func (p *Payment) IsGuest() bool {
	return p.UserID.IsZero()
}

// IsSettled reports whether the attempt has reached a final state, after
// which provider callbacks are ignored.
func (p *Payment) IsSettled() bool {
	switch p.Status {
	case StatusSuccessful, StatusRefunded:
		return true
	default:
		return false
	}
}

// IsRefundable reports whether the attempt can be refunded.
func (p *Payment) IsRefundable() bool {
	return p.Status == StatusSuccessful
}

// Repository is the persistence contract for payment attempts.
type Repository interface {
	// Create inserts an attempt; the reference is unique.
	Create(ctx context.Context, payment *Payment) error

	// FindByReference returns an attempt or ErrPaymentNotFound.
	FindByReference(ctx context.Context, reference string) (*Payment, error)

	// FindByID returns an attempt or ErrPaymentNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Payment, error)

	// ListForUser returns a customer's transaction history.
	ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]Payment, int64, error)

	// List returns attempts for the admin dashboard.
	List(ctx context.Context, filter Filter, page, limit int) ([]Payment, int64, error)

	// Settle moves an attempt to a final state, but only from a non-final
	// one. It reports whether this call applied the change, which is what
	// makes duplicate webhooks idempotent.
	Settle(ctx context.Context, reference string, update SettlementUpdate) (bool, error)

	// MarkRefunded flips a successful attempt to refunded.
	MarkRefunded(ctx context.Context, id bson.ObjectID) (bool, error)

	// ReferenceExists reports whether a reference is already taken.
	ReferenceExists(ctx context.Context, reference string) (bool, error)
}

// SettlementUpdate carries the provider's verdict on an attempt.
type SettlementUpdate struct {
	Status            string
	ProviderReference string
	Channel           string
	GatewayResponse   string
	PaidAt            *time.Time
}

// Filter narrows an administrative payment listing.
type Filter struct {
	UserID  *bson.ObjectID
	OrderID *bson.ObjectID
	Status  string
	Search  string
}
