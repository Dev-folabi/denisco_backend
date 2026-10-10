// Package application coordinates the consultation workflows: publishing the
// availability grid, taking bookings against it and managing them.
package application

import (
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Actor identifies the administrator behind a change.
type Actor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}

// TypeView is the public projection of a consultation service.
type TypeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Duration is the label the prototype shows ("60 mins").
	Duration        string `json:"duration"`
	DurationMinutes int    `json:"duration_minutes"`
	Price           int64  `json:"price"`
	Currency        string `json:"currency"`
	Active          bool   `json:"active"`
	Position        int    `json:"position"`
}

// newTypeView projects a consultation type.
func newTypeView(t domain.Type) TypeView {
	return TypeView{
		ID:              t.ID.Hex(),
		Name:            t.Name,
		Description:     t.Description,
		Duration:        t.DurationLabel(),
		DurationMinutes: t.DurationMinutes,
		Price:           t.Price,
		Currency:        t.Currency,
		Active:          t.Active,
		Position:        t.Position,
	}
}

// SlotView is one offered time, with whether it can still be booked.
type SlotView struct {
	ID   string `json:"id"`
	Date string `json:"date"`
	Time string `json:"time"`
	// Available is false when any consultation is already booked at this date
	// and time: a slot holds one session, whichever service took it.
	Available bool `json:"available"`
}

// AvailabilityView is the booking calendar: the dates on offer, the times
// offered on each, and which of them are still free.
type AvailabilityView struct {
	Dates []string   `json:"dates"`
	Times []string   `json:"times"`
	Slots []SlotView `json:"slots"`
}

// BookingView is the public projection of a booking.
type BookingView struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	UserID    string `json:"user_id,omitempty"`
	TypeID    string `json:"type_id"`
	TypeName  string `json:"type_name"`
	TypePrice int64  `json:"type_price"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Notes     string `json:"notes,omitempty"`
	Status    string `json:"status"`
	// PaymentStatus is the state of the consultation fee: "not_required" for a
	// free service, otherwise "pending", "paid", "failed" or "refunded".
	PaymentStatus string  `json:"payment_status"`
	PaymentRef    string  `json:"payment_ref,omitempty"`
	PaidAt        *string `json:"paid_at,omitempty"`
	// Payable is true while the fee can still be settled, so a client can
	// offer a "pay now" action without restating the rule.
	Payable   bool   `json:"payable"`
	CreatedAt string `json:"created_at"`
}

// newBookingView projects a booking.
func newBookingView(b domain.Booking) BookingView {
	view := BookingView{
		ID:        b.ID.Hex(),
		Reference: b.Reference,
		TypeID:    b.TypeID.Hex(),
		TypeName:  b.TypeName,
		TypePrice: b.TypePrice,
		Date:      b.Date,
		Time:      b.Time,
		Name:      b.Name,
		Email:     b.Email,
		Phone:     b.Phone,
		Notes:     b.Notes,
		Status:    b.Status,
		// Bookings written before consultations were payable have no stored
		// status; a free service is the only way that can happen now.
		PaymentStatus: paymentStatusOr(b),
		PaymentRef:    b.PaymentRef,
		Payable:       b.IsPayable(),
		CreatedAt:     b.CreatedAt.UTC().Format(time.RFC3339),
	}
	if b.UserID != nil {
		view.UserID = b.UserID.Hex()
	}
	if b.PaidAt != nil {
		paidAt := b.PaidAt.UTC().Format(time.RFC3339)
		view.PaidAt = &paidAt
	}
	return view
}

// paymentStatusOr fills in the payment status of a booking stored before the
// field existed, so no client has to treat it as optional.
func paymentStatusOr(b domain.Booking) string {
	if b.PaymentStatus != "" {
		return b.PaymentStatus
	}
	return domain.InitialPaymentStatus(b.TypePrice)
}

// toBookingViews projects a slice of bookings.
func toBookingViews(bookings []domain.Booking) []BookingView {
	views := make([]BookingView, 0, len(bookings))
	for _, booking := range bookings {
		views = append(views, newBookingView(booking))
	}
	return views
}

// BookInput is a request to reserve a consultation slot.
type BookInput struct {
	TypeID string
	Date   string
	Time   string
	Name   string
	Email  string
	Phone  string
	Notes  string
	// UserID is set when the visitor is signed in. The consultation page is
	// public, so a guest booking simply carries no account.
	UserID *bson.ObjectID
}

// TypeInput creates or updates a consultation service.
type TypeInput struct {
	Name            *string
	Description     *string
	DurationMinutes *int
	Price           *int64
	Active          *bool
	Position        *int
}

// SlotsInput adds or removes availability.
//
// Either list may be empty: adding dates with no times uses the times already
// on offer, and removing a date clears every time on it.
type SlotsInput struct {
	Dates []string
	Times []string
}
