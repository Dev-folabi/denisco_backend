package application

import (
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
)

// SalesPoint is one bar on the dashboard's revenue chart.
type SalesPoint struct {
	Date    string `json:"date"`
	Label   string `json:"label"`
	Revenue int64  `json:"revenue"`
}

// RecentOrder is the compact order row in the dashboard's activity list.
type RecentOrder struct {
	ID            string `json:"id"`
	OrderNumber   string `json:"order_number"`
	CustomerName  string `json:"customer_name"`
	Total         int64  `json:"total"`
	Status        string `json:"status"`
	PaymentStatus string `json:"payment_status"`
	CreatedAt     string `json:"created_at"`
}

// toRecentOrders projects orders for the dashboard's activity list.
func toRecentOrders(orders []ordersdomain.Order) []RecentOrder {
	rows := make([]RecentOrder, 0, len(orders))
	for _, order := range orders {
		rows = append(rows, RecentOrder{
			ID:            order.ID.Hex(),
			OrderNumber:   order.OrderNumber,
			CustomerName:  order.Customer.Name,
			Total:         order.Total,
			Status:        order.Status,
			PaymentStatus: order.PaymentStatus,
			CreatedAt:     order.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return rows
}

// OverviewView is the admin dashboard payload.
type OverviewView struct {
	TotalRevenue         int64                    `json:"total_revenue"`
	TotalOrders          int64                    `json:"total_orders"`
	TotalCustomers       int64                    `json:"total_customers"`
	TotalProducts        int64                    `json:"total_products"`
	PendingOrders        int64                    `json:"pending_orders"`
	ConsultationBookings int64                    `json:"consultation_bookings"`
	Currency             string                   `json:"currency"`
	RecentOrders         []RecentOrder            `json:"recent_orders"`
	RecentBookings       []consultapp.BookingView `json:"recent_bookings"`
	Sales                []SalesPoint             `json:"sales"`
}

// CustomerView is the admin projection of a customer account.
type CustomerView struct {
	ID            string `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Status        string `json:"status"`
	EmailVerified bool   `json:"email_verified"`
	CreatedAt     string `json:"created_at"`
	LastLoginAt   string `json:"last_login_at,omitempty"`
}

// newCustomerView projects a user account for the admin dashboard.
func newCustomerView(user usersdomain.User) CustomerView {
	view := CustomerView{
		ID:            user.ID.Hex(),
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		FullName:      user.FullName(),
		Email:         user.Email,
		Phone:         user.Phone,
		Status:        user.Status,
		EmailVerified: user.EmailVerified,
		CreatedAt:     user.CreatedAt.UTC().Format(time.RFC3339),
	}
	if user.LastLoginAt != nil {
		view.LastLoginAt = user.LastLoginAt.UTC().Format(time.RFC3339)
	}
	return view
}

// CustomerDetailView is a customer's profile with their trading history.
type CustomerDetailView struct {
	Customer    *CustomerView            `json:"customer,omitempty"`
	Orders      []ordersapp.View         `json:"orders"`
	Bookings    []consultapp.BookingView `json:"bookings"`
	TotalOrders int64                    `json:"total_orders"`
	TotalSpent  int64                    `json:"total_spent"`
}

// AuditView is one entry in the administrative audit trail.
type AuditView struct {
	ID         string         `json:"id"`
	ActorID    string         `json:"actor_id,omitempty"`
	ActorEmail string         `json:"actor_email,omitempty"`
	ActorRole  string         `json:"actor_role,omitempty"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	ResourceID string         `json:"resource_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// newAuditView projects an audit entry.
func newAuditView(entry audit.Entry) AuditView {
	view := AuditView{
		ID:         entry.ID.Hex(),
		ActorEmail: entry.ActorEmail,
		ActorRole:  entry.ActorRole,
		Action:     entry.Action,
		Resource:   entry.Resource,
		ResourceID: entry.ResourceID,
		Metadata:   entry.Metadata,
		CreatedAt:  entry.CreatedAt.UTC().Format(time.RFC3339),
	}
	if entry.ActorID != nil {
		view.ActorID = entry.ActorID.Hex()
	}
	return view
}

// consultLocation exposes the farm's timezone to the dashboard's date
// bucketing, so the sales chart's days match the trading days.
func consultLocation() *time.Location {
	return consultdomain.Lagos
}
