// Package domain holds the consultation entities: the services offered, the
// availability grid and the bookings customers place against it.
package domain

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// BookingDaysAhead is how far ahead the booking calendar runs, from the
// prototype's consultation schedule.
const BookingDaysAhead = 31

// Lagos is the farm's timezone. Timestamps are stored in UTC and dates are
// interpreted here, so "today" means today in Nigeria rather than on the
// server.
var Lagos = mustLoadLagos()

// Booking statuses, matching the admin dashboard's dropdown.
const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusNoShow    = "no_show"
)

// Booking payment statuses. A consultation is paid for through Paystack the
// same way an order is, so the booking carries the settled state of its latest
// attempt rather than the attempts themselves, which live in the payments
// module.
const (
	// PaymentNotRequired is used by a free consultation type, so the admin
	// table distinguishes "nothing to pay" from "not paid yet".
	PaymentNotRequired = "not_required"
	PaymentPending     = "pending"
	PaymentPaid        = "paid"
	PaymentFailed      = "failed"
	PaymentRefunded    = "refunded"
)

// Currency is fixed to naira; prices are stored in kobo.
const Currency = "NGN"

// Consultation errors.
var (
	ErrTypeNotFound    = apperror.New(http.StatusNotFound, "CONSULTATION_TYPE_NOT_FOUND", "The consultation type could not be found.")
	ErrBookingNotFound = apperror.New(http.StatusNotFound, "BOOKING_NOT_FOUND", "The booking could not be found.")
	ErrSlotUnavailable = apperror.New(http.StatusConflict, "SLOT_UNAVAILABLE", "That time slot has just been taken. Please choose another.")
	ErrSlotNotOffered  = apperror.New(http.StatusUnprocessableEntity, "SLOT_NOT_OFFERED", "That date and time is not available for booking.")
	ErrNotCancellable  = apperror.New(http.StatusConflict, "BOOKING_NOT_CANCELLABLE", "This booking can no longer be cancelled.")
	ErrInvalidStatus   = apperror.New(http.StatusUnprocessableEntity, "INVALID_BOOKING_STATUS", "That is not a valid booking status.")
	ErrDateInPast      = apperror.New(http.StatusUnprocessableEntity, "DATE_IN_PAST", "That date has already passed.")
	ErrTypeInUse       = apperror.New(http.StatusConflict, "CONSULTATION_TYPE_IN_USE", "This consultation type has bookings and cannot be deleted. Deactivate it instead.")
	ErrNotPayable      = apperror.New(http.StatusConflict, "BOOKING_NOT_PAYABLE", "This booking has nothing left to pay.")
)

// Type is a consultation service the farm offers.
type Type struct {
	ID bson.ObjectID `bson:"_id,omitempty"`
	// Code is the stable identifier from the prototype ("CT-01"), kept so
	// seeded types can be matched across re-runs.
	Code        string `bson:"code,omitempty"`
	Name        string `bson:"name"`
	Description string `bson:"description"`
	// DurationMinutes is stored as a number so the UI can format it; the
	// prototype displays it as "60 mins".
	DurationMinutes int       `bson:"duration_minutes"`
	Price           int64     `bson:"price"` // kobo
	Currency        string    `bson:"currency"`
	Active          bool      `bson:"active"`
	Position        int       `bson:"position"`
	CreatedAt       time.Time `bson:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at"`
}

// DurationLabel renders the duration the way the prototype does.
func (t *Type) DurationLabel() string {
	return strconv.Itoa(t.DurationMinutes) + " mins"
}

// Slot is one offered date and time. Availability is a grid shared by every
// consultation type, and a slot holds one session: once any service is booked
// at a date and time, that time is closed to every other service too, because
// the farm can only run one consultation at once.
type Slot struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Date      string        `bson:"date"` // YYYY-MM-DD, in Africa/Lagos
	Time      string        `bson:"time"` // "09:00 AM"
	CreatedAt time.Time     `bson:"created_at"`
}

// Booking is a customer's reserved consultation.
//
// UserID is optional: the consultation page is public in the prototype, so a
// visitor can book without an account and simply has no booking history.
type Booking struct {
	ID        bson.ObjectID  `bson:"_id,omitempty"`
	Reference string         `bson:"reference"`
	UserID    *bson.ObjectID `bson:"user_id,omitempty"`
	TypeID    bson.ObjectID  `bson:"type_id"`
	TypeName  string         `bson:"type_name"`
	TypePrice int64          `bson:"type_price"`
	Date      string         `bson:"date"`
	Time      string         `bson:"time"`
	Name      string         `bson:"name"`
	Email     string         `bson:"email"`
	Phone     string         `bson:"phone"`
	Notes     string         `bson:"notes,omitempty"`
	Status    string         `bson:"status"`
	// PaymentStatus tracks the consultation fee. It is kept on the booking,
	// rather than derived from the payments collection, so the admin table and
	// the customer's history need no join.
	PaymentStatus string     `bson:"payment_status"`
	PaymentRef    string     `bson:"payment_ref,omitempty"`
	PaidAt        *time.Time `bson:"paid_at,omitempty"`
	// Active is set while the booking holds its slot and unset once it is
	// cancelled. A partial unique index on (date, time) filtered to active
	// bookings is what makes double-booking impossible rather than merely
	// unlikely — including across consultation types.
	Active      *bool      `bson:"active,omitempty"`
	CancelledAt *time.Time `bson:"cancelled_at,omitempty"`
	CreatedAt   time.Time  `bson:"created_at"`
	UpdatedAt   time.Time  `bson:"updated_at"`
}

// HoldsSlot reports whether the booking still occupies its slot.
func (b *Booking) HoldsSlot() bool {
	return b.Active != nil && *b.Active
}

// IsCancellable reports whether the customer may still cancel.
func (b *Booking) IsCancellable() bool {
	return b.Status == StatusPending || b.Status == StatusConfirmed
}

// IsPayable reports whether a fee is still outstanding on this booking.
//
// A failed attempt is payable again: the customer keeps the slot and may
// retry, which is the same rule orders follow after a declined card.
func (b *Booking) IsPayable() bool {
	if b.TypePrice <= 0 {
		return false
	}
	if b.Status == StatusCancelled || b.Status == StatusNoShow {
		return false
	}
	switch b.PaymentStatus {
	case PaymentPending, PaymentFailed, "":
		return true
	default:
		return false
	}
}

// InitialPaymentStatus is the state a new booking starts in: nothing to pay
// for a free consultation, otherwise an outstanding fee.
func InitialPaymentStatus(price int64) string {
	if price <= 0 {
		return PaymentNotRequired
	}
	return PaymentPending
}

// ValidateStatus checks a booking status name.
func ValidateStatus(status string) error {
	switch status {
	case StatusPending, StatusConfirmed, StatusCompleted, StatusCancelled, StatusNoShow:
		return nil
	default:
		return ErrInvalidStatus
	}
}

// ReleasesSlot reports whether moving to a status should free the slot for
// someone else.
func ReleasesSlot(status string) bool {
	return status == StatusCancelled || status == StatusNoShow
}

// datePattern is the calendar-date format used throughout the booking flow.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidateDate checks a YYYY-MM-DD date and rejects dates in the past or
// beyond the booking window.
func ValidateDate(date string) (string, error) {
	trimmed := strings.TrimSpace(date)
	if !datePattern.MatchString(trimmed) {
		return "", apperror.Validation.WithDetails(map[string]any{"date": "must be in YYYY-MM-DD format"})
	}

	parsed, err := time.ParseInLocation("2006-01-02", trimmed, Lagos)
	if err != nil {
		return "", apperror.Validation.WithDetails(map[string]any{"date": "is not a real date"})
	}

	today := Today()
	if parsed.Before(today) {
		return "", ErrDateInPast
	}
	if parsed.After(today.AddDate(0, 0, BookingDaysAhead)) {
		return "", apperror.Validation.WithDetails(map[string]any{
			"date": "is beyond the booking window",
		})
	}

	return trimmed, nil
}

// Today returns midnight today in the farm's timezone.
func Today() time.Time {
	now := time.Now().In(Lagos)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, Lagos)
}

// TodayString returns today's calendar date in the farm's timezone.
func TodayString() string {
	return Today().Format("2006-01-02")
}

// timePattern matches the 12-hour labels the prototype uses.
var timePattern = regexp.MustCompile(`^(0[1-9]|1[0-2]):[0-5][0-9] (AM|PM)$`)

// NormalizeTime accepts either a 24-hour value from a time input ("13:00") or
// an existing label ("01:00 PM") and returns the label form the UI displays.
func NormalizeTime(value string) (string, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))

	if timePattern.MatchString(trimmed) {
		return trimmed, nil
	}

	parsed, err := time.Parse("15:04", trimmed)
	if err != nil {
		return "", apperror.Validation.WithDetails(map[string]any{
			"time": "must look like \"09:00 AM\" or \"13:00\"",
		})
	}

	return parsed.Format("03:04 PM"), nil
}

// TimeToMinutes converts a label to minutes past midnight, for sorting.
func TimeToMinutes(label string) int {
	parsed, err := time.Parse("03:04 PM", label)
	if err != nil {
		return 0
	}
	return parsed.Hour()*60 + parsed.Minute()
}

// DefaultTimes are the slots the prototype offers out of the box.
func DefaultTimes() []string {
	return []string{"09:00 AM", "11:00 AM", "01:00 PM", "03:00 PM"}
}

// SeedTypes returns the consultation services defined by the prototype, with
// prices converted from naira to kobo.
func SeedTypes() []Type {
	return []Type{
		{
			Code: "CT-01", Name: "General Farm Setup Consultation",
			DurationMinutes: 60, Price: 15_000 * 100, Position: 1, Active: true,
			Description: "One-on-one guidance for individuals or groups planning to start a poultry, livestock, or crop farming business.",
		},
		{
			Code: "CT-02", Name: "Poultry Health & Management Advisory",
			DurationMinutes: 45, Price: 10_000 * 100, Position: 2, Active: true,
			Description: "Practical advice on poultry housing, feeding, disease prevention and flock management.",
		},
		{
			Code: "CT-03", Name: "Livestock (Ruminant) Management Consultation",
			DurationMinutes: 60, Price: 15_000 * 100, Position: 3, Active: true,
			Description: "Expert guidance on cattle, goat, sheep and ram rearing, breeding and pasture management.",
		},
		{
			Code: "CT-04", Name: "Piggery & Snail Farming Consultation",
			DurationMinutes: 45, Price: 10_000 * 100, Position: 4, Active: true,
			Description: "Support for setting up or improving a piggery or snail farming operation, including housing and feeding.",
		},
		{
			Code: "CT-05", Name: "Crop Production & Soil Advisory",
			DurationMinutes: 60, Price: 15_000 * 100, Position: 5, Active: true,
			Description: "Guidance on land preparation, crop selection, planting schedules and post-harvest handling.",
		},
	}
}

// mustLoadLagos resolves the farm's timezone, falling back to a fixed +01:00
// offset when the host has no timezone database (a slim container image).
func mustLoadLagos() *time.Location {
	if loc, err := time.LoadLocation("Africa/Lagos"); err == nil {
		return loc
	}
	return time.FixedZone("WAT", 60*60)
}
