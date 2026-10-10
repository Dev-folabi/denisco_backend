package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TypeRepository persists consultation services.
type TypeRepository interface {
	// List returns types in display order; activeOnly hides retired ones.
	List(ctx context.Context, activeOnly bool) ([]Type, error)

	// FindByID returns a type or ErrTypeNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Type, error)

	// Create inserts a type.
	Create(ctx context.Context, consultationType *Type) error

	// Update applies a set of changed fields.
	Update(ctx context.Context, id bson.ObjectID, fields bson.M) error

	// Delete removes a type outright. The caller refuses when bookings exist.
	Delete(ctx context.Context, id bson.ObjectID) error

	// Upsert inserts or refreshes a type by its seed code.
	Upsert(ctx context.Context, consultationType *Type) error
}

// SlotRepository persists the availability grid.
type SlotRepository interface {
	// List returns slots within an inclusive date range, ordered by date then
	// time. An empty `from`/`to` means unbounded.
	List(ctx context.Context, from, to string) ([]Slot, error)

	// Exists reports whether a date and time are offered.
	Exists(ctx context.Context, date, slotTime string) (bool, error)

	// Add creates slots for every date and time combination, ignoring ones
	// that already exist.
	Add(ctx context.Context, dates, times []string) (int, error)

	// Remove deletes slots matching the given dates and times. Empty means
	// "any", so removing a date clears every time on it.
	Remove(ctx context.Context, dates, times []string) (int64, error)

	// DeleteByID removes one slot.
	DeleteByID(ctx context.Context, id bson.ObjectID) error

	// DistinctTimes returns the times currently offered, sorted.
	DistinctTimes(ctx context.Context) ([]string, error)

	// PurgeBefore removes slots for dates that have passed.
	PurgeBefore(ctx context.Context, date string) (int64, error)
}

// BookingRepository persists consultation bookings.
type BookingRepository interface {
	// Create inserts a booking. It returns ErrSlotUnavailable when the slot's
	// unique index rejects the insert, which is how two simultaneous bookings
	// for the same slot are resolved.
	Create(ctx context.Context, booking *Booking) error

	// FindByID returns a booking or ErrBookingNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Booking, error)

	// ListForUser returns a customer's bookings, newest first.
	ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]Booking, int64, error)

	// List returns bookings for the admin dashboard.
	List(ctx context.Context, filter BookingFilter, page, limit int) ([]Booking, int64, error)

	// TakenSlots returns the (date, time) pairs already held by a live
	// booking within a date range, for any consultation type: a booked time
	// is closed to every service, not just the one that took it.
	TakenSlots(ctx context.Context, from, to string) (map[string]bool, error)

	// UpdateStatus changes a booking's status, releasing or keeping its slot.
	// It reports whether the change applied.
	UpdateStatus(ctx context.Context, id bson.ObjectID, status string, releaseSlot bool) (bool, error)

	// CountForType reports how many bookings reference a consultation type.
	CountForType(ctx context.Context, typeID bson.ObjectID) (int64, error)

	// MarkPaid records a settled consultation fee, but only while the fee is
	// outstanding, and reports whether this call applied the change. That is
	// what makes a replayed payment webhook idempotent.
	MarkPaid(ctx context.Context, id bson.ObjectID, reference string, paidAt time.Time) (bool, error)

	// MarkPaymentFailed records a declined or abandoned attempt. The booking
	// keeps its slot so the customer can retry.
	MarkPaymentFailed(ctx context.Context, id bson.ObjectID, status string) (bool, error)

	// MarkRefunded records a refunded fee against a paid booking.
	MarkRefunded(ctx context.Context, id bson.ObjectID) (bool, error)

	// ReferenceExists reports whether a booking reference is taken.
	ReferenceExists(ctx context.Context, reference string) (bool, error)

	// Count returns the total number of bookings, for the dashboard.
	Count(ctx context.Context) (int64, error)

	// Recent returns the newest bookings, for the dashboard.
	Recent(ctx context.Context, limit int) ([]Booking, error)
}

// BookingFilter narrows an administrative booking listing.
type BookingFilter struct {
	Status string
	TypeID *bson.ObjectID
	Date   string
	// Search matches the reference, customer name or email.
	Search string
}
