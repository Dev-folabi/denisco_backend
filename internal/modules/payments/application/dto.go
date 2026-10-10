package application

import (
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Actor identifies the administrator behind a refund.
type Actor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}

// InitializeView is the checkout handover returned to the storefront.
//
// Either the order or the booking fields are filled, according to `purpose`,
// so the callback page knows what it is confirming.
type InitializeView struct {
	Reference        string `json:"reference"`
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code,omitempty"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	Purpose          string `json:"purpose"`
	OrderID          string `json:"order_id,omitempty"`
	OrderNumber      string `json:"order_number,omitempty"`
	BookingID        string `json:"booking_id,omitempty"`
	BookingReference string `json:"booking_reference,omitempty"`
}

// View is the public projection of a payment attempt.
type View struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	// Purpose is "order" or "consultation".
	Purpose          string  `json:"purpose"`
	OrderID          string  `json:"order_id,omitempty"`
	OrderNumber      string  `json:"order_number,omitempty"`
	BookingID        string  `json:"booking_id,omitempty"`
	BookingReference string  `json:"booking_reference,omitempty"`
	UserID           string  `json:"user_id,omitempty"`
	CustomerName     string  `json:"customer_name,omitempty"`
	Email            string  `json:"email,omitempty"`
	Amount           int64   `json:"amount"`
	Currency         string  `json:"currency"`
	Status           string  `json:"status"`
	Provider         string  `json:"provider"`
	Method           string  `json:"method,omitempty"`
	Channel          string  `json:"channel,omitempty"`
	PaidAt           *string `json:"paid_at,omitempty"`
	RefundedAt       *string `json:"refunded_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// newView projects a payment attempt into its API representation.
func newView(record *domain.Payment) View {
	view := View{
		ID:               record.ID.Hex(),
		Reference:        record.Reference,
		Purpose:          record.Kind(),
		OrderID:          hexOrEmpty(record.OrderID),
		OrderNumber:      record.OrderNumber,
		BookingID:        hexOrEmpty(record.BookingID),
		BookingReference: record.BookingReference,
		UserID:           hexOrEmpty(record.UserID),
		CustomerName:     record.CustomerName,
		Email:            record.Email,
		Amount:           record.Amount,
		Currency:         record.Currency,
		Status:           record.Status,
		Provider:         record.Provider,
		Channel:          record.Channel,
		CreatedAt:        record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        record.UpdatedAt.UTC().Format(time.RFC3339),
	}

	// The transaction tables label the payment method; Paystack's channel
	// ("card", "bank_transfer") is the closest thing it reports.
	view.Method = record.Channel
	if view.Method == "" {
		view.Method = "Paystack"
	}

	if record.PaidAt != nil {
		paidAt := record.PaidAt.UTC().Format(time.RFC3339)
		view.PaidAt = &paidAt
	}
	if record.RefundedAt != nil {
		refundedAt := record.RefundedAt.UTC().Format(time.RFC3339)
		view.RefundedAt = &refundedAt
	}

	return view
}

// toViews projects a slice of attempts.
func toViews(payments []domain.Payment) []View {
	views := make([]View, 0, len(payments))
	for i := range payments {
		views = append(views, newView(&payments[i]))
	}
	return views
}
