package application

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	"github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Outbox event types emitted by the booking lifecycle.
const (
	EventBookingCreated       = "consultation.booked"
	EventBookingCancelled     = "consultation.cancelled"
	EventBookingStatusChanged = "consultation.status_changed"
	// EventBookingRefunded and EventBookingPaymentFailed are customer notices
	// about the fee: a paid fee was returned, or an attempt did not go through
	// and the slot is still held.
	EventBookingRefunded      = "consultation.refunded"
	EventBookingPaymentFailed = "consultation.payment_failed"
)

// referenceAttempts bounds the retry loop for a unique booking reference.
const referenceAttempts = 10

// Audit actions for consultation management.
const (
	actionTypeCreated   = "consultation_type.created"
	actionTypeUpdated   = "consultation_type.updated"
	actionTypeDeleted   = "consultation_type.deleted"
	actionSlotsChanged  = "consultation_slots.changed"
	actionBookingStatus = "consultation_booking.status_changed"
)

// Service implements the consultation workflows.
type Service struct {
	types    domain.TypeRepository
	slots    domain.SlotRepository
	bookings domain.BookingRepository
	events   outbox.Writer
	auditor  audit.Recorder
}

// NewService wires the module to its repositories.
func NewService(
	types domain.TypeRepository,
	slots domain.SlotRepository,
	bookings domain.BookingRepository,
	events outbox.Writer,
	auditor audit.Recorder,
) *Service {
	return &Service{types: types, slots: slots, bookings: bookings, events: events, auditor: auditor}
}

// ListTypes returns the consultation services on offer.
func (s *Service) ListTypes(ctx context.Context, activeOnly bool) ([]TypeView, error) {
	types, err := s.types.List(ctx, activeOnly)
	if err != nil {
		return nil, apperror.From(err)
	}

	views := make([]TypeView, 0, len(types))
	for _, t := range types {
		views = append(views, newTypeView(t))
	}
	return views, nil
}

// Availability returns the booking calendar.
//
// A slot holds one session: a time already booked is reported unavailable to
// every consultation type, not just the one that took it, because the farm
// cannot run two consultations at once. The consultation type is therefore no
// longer part of the answer, and the `type_id` parameter is accepted only so
// existing clients keep working.
func (s *Service) Availability(ctx context.Context, typeID string) (*AvailabilityView, error) {
	from := domain.TodayString()
	to := domain.Today().AddDate(0, 0, domain.BookingDaysAhead).Format("2006-01-02")

	slots, err := s.slots.List(ctx, from, to)
	if err != nil {
		return nil, apperror.From(err)
	}

	taken, err := s.bookings.TakenSlots(ctx, from, to)
	if err != nil {
		return nil, apperror.From(err)
	}

	view := &AvailabilityView{Dates: []string{}, Times: []string{}, Slots: []SlotView{}}

	seenDates := map[string]bool{}
	seenTimes := map[string]bool{}

	for _, slot := range slots {
		if !seenDates[slot.Date] {
			seenDates[slot.Date] = true
			view.Dates = append(view.Dates, slot.Date)
		}
		if !seenTimes[slot.Time] {
			seenTimes[slot.Time] = true
			view.Times = append(view.Times, slot.Time)
		}

		view.Slots = append(view.Slots, SlotView{
			ID:        slot.ID.Hex(),
			Date:      slot.Date,
			Time:      slot.Time,
			Available: !taken[slot.Date+"|"+slot.Time],
		})
	}

	sort.SliceStable(view.Times, func(i, j int) bool {
		return domain.TimeToMinutes(view.Times[i]) < domain.TimeToMinutes(view.Times[j])
	})

	return view, nil
}

// Book reserves a slot.
//
// There is no read-then-write check: the insert either succeeds or is rejected
// by the partial unique index on (date, time), so two customers submitting the
// same slot at the same moment cannot both win — and neither can two customers
// submitting the same time for different services.
func (s *Service) Book(ctx context.Context, input BookInput) (*BookingView, error) {
	typeID, err := bson.ObjectIDFromHex(strings.TrimSpace(input.TypeID))
	if err != nil {
		return nil, domain.ErrTypeNotFound
	}

	consultationType, err := s.types.FindByID(ctx, typeID)
	if err != nil {
		return nil, apperror.From(err)
	}
	if !consultationType.Active {
		return nil, apperror.New(http.StatusUnprocessableEntity, "CONSULTATION_TYPE_UNAVAILABLE",
			"This consultation service is not currently offered.")
	}

	date, err := domain.ValidateDate(input.Date)
	if err != nil {
		return nil, err
	}

	slotTime, err := domain.NormalizeTime(input.Time)
	if err != nil {
		return nil, err
	}

	// The slot has to be one the farm actually offers.
	offered, err := s.slots.Exists(ctx, date, slotTime)
	if err != nil {
		return nil, apperror.From(err)
	}
	if !offered {
		return nil, domain.ErrSlotNotOffered
	}

	name, err := usersdomain.ValidateName("name", input.Name)
	if err != nil {
		return nil, err
	}

	email, err := usersdomain.ValidateEmail(input.Email)
	if err != nil {
		return nil, err
	}

	phone, err := usersdomain.ValidatePhone(input.Phone)
	if err != nil {
		return nil, err
	}

	notes := strings.TrimSpace(input.Notes)
	if len(notes) > 1000 {
		return nil, apperror.Validation.WithDetails(map[string]any{"notes": "must be 1000 characters or fewer"})
	}

	reference, err := s.newReference(ctx)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	active := true
	booking := &domain.Booking{
		ID:        bson.NewObjectID(),
		Reference: reference,
		UserID:    input.UserID,
		TypeID:    consultationType.ID,
		TypeName:  consultationType.Name,
		TypePrice: consultationType.Price,
		Date:      date,
		Time:      slotTime,
		Name:      name,
		Email:     email,
		Phone:     phone,
		Notes:     notes,
		Status:    domain.StatusPending,
		// The fee is outstanding until Paystack confirms it; a free
		// consultation type has nothing to pay.
		PaymentStatus: domain.InitialPaymentStatus(consultationType.Price),
		Active:        &active,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.bookings.Create(ctx, booking); err != nil {
		return nil, apperror.From(err)
	}

	s.emit(ctx, EventBookingCreated, booking)

	logger.FromContext(ctx).Info("consultation booked",
		slog.String("module", "consultations"),
		slog.String("booking_id", booking.ID.Hex()),
		slog.String("reference", booking.Reference),
		slog.String("date", booking.Date),
		slog.String("time", booking.Time),
	)

	view := newBookingView(*booking)
	return &view, nil
}

// BookingForPayment loads a booking that still owes its fee, for the payments
// module to open a Paystack attempt against.
//
// Authorization has to cover guests, because the consultation page is public:
// a signed-in customer must own the booking, an administrator may act on any,
// and a guest booking is matched on its reference as well as its identifier.
// The reference is not a secret, but requiring both means a caller cannot walk
// identifiers and learn which ones exist.
func (s *Service) BookingForPayment(ctx context.Context, id, reference string, viewer Viewer) (*domain.Booking, error) {
	booking, err := s.loadBooking(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorizeWithReference(booking, viewer, reference); err != nil {
		return nil, err
	}

	if !booking.IsPayable() {
		return nil, domain.ErrNotPayable
	}

	return booking, nil
}

// MarkBookingPaid settles a confirmed consultation fee.
//
// It reports nothing back to the caller beyond an error: the payments module
// owns the attempt and this is the booking's side of it.
func (s *Service) MarkBookingPaid(ctx context.Context, id bson.ObjectID, reference string, paidAt time.Time) error {
	applied, err := s.bookings.MarkPaid(ctx, id, reference, paidAt)
	if err != nil {
		return apperror.From(err)
	}
	if !applied {
		// Already settled, or cancelled before the money arrived. Both are
		// recorded against the payment, so there is nothing to do here.
		logger.FromContext(ctx).Warn("consultation payment applied to no booking",
			slog.String("module", "consultations"),
			slog.String("booking_id", id.Hex()),
			slog.String("payment_reference", reference),
		)
		return nil
	}

	booking, err := s.bookings.FindByID(ctx, id)
	if err != nil {
		return apperror.From(err)
	}

	s.emit(ctx, EventBookingStatusChanged, booking)

	logger.FromContext(ctx).Info("consultation fee paid",
		slog.String("module", "consultations"),
		slog.String("booking_id", id.Hex()),
		slog.String("reference", booking.Reference),
		slog.String("payment_reference", reference),
	)

	return nil
}

// MarkBookingPaymentFailed records a declined or abandoned attempt. The slot
// stays held so the customer can retry, and they are told it can be retried —
// a fee that quietly fails looks to the customer like a booking that vanished.
func (s *Service) MarkBookingPaymentFailed(ctx context.Context, id bson.ObjectID, status string) error {
	if status != domain.PaymentFailed {
		// Abandoned and expired attempts read the same way to the farm: the
		// fee is still owed.
		status = domain.PaymentFailed
	}

	if _, err := s.bookings.MarkPaymentFailed(ctx, id, status); err != nil {
		return apperror.From(err)
	}

	s.emitBookingEvent(ctx, id, EventBookingPaymentFailed)

	logger.FromContext(ctx).Info("consultation fee not paid",
		slog.String("module", "consultations"),
		slog.String("booking_id", id.Hex()),
	)

	return nil
}

// MarkBookingRefunded records a refunded fee and tells the customer.
func (s *Service) MarkBookingRefunded(ctx context.Context, id bson.ObjectID) error {
	if _, err := s.bookings.MarkRefunded(ctx, id); err != nil {
		return apperror.From(err)
	}

	s.emitBookingEvent(ctx, id, EventBookingRefunded)
	return nil
}

// emitBookingEvent re-reads a booking to build its notification.
//
// The state change has already committed, so a failure to reload is logged
// rather than returned: the fee is refunded either way, and losing the email
// is a lesser fault than reporting the refund as failed.
func (s *Service) emitBookingEvent(ctx context.Context, id bson.ObjectID, eventType string) {
	booking, err := s.bookings.FindByID(ctx, id)
	if err != nil {
		logger.FromContext(ctx).Warn("could not reload booking for notification",
			slog.String("module", "consultations"),
			slog.String("event_type", eventType),
			slog.String("booking_id", id.Hex()),
			slog.String("error", err.Error()),
		)
		return
	}

	s.emit(ctx, eventType, booking)
}

// GetBooking returns one booking, enforcing ownership.
func (s *Service) GetBooking(ctx context.Context, id string, viewer Viewer) (*BookingView, error) {
	return s.GetBookingWithReference(ctx, id, "", viewer)
}

// GetBookingWithReference returns one booking, letting a guest read theirs by
// presenting its reference.
//
// A guest booking belongs to no account, so there is no session to check it
// against; the confirmation page is reached holding both the identifier and
// the reference, and requiring both is what keeps identifiers from being
// walked. A signed-in customer still has to own the booking: a reference is
// not a way around that.
func (s *Service) GetBookingWithReference(ctx context.Context, id, reference string, viewer Viewer) (*BookingView, error) {
	booking, err := s.loadBooking(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorizeWithReference(booking, viewer, reference); err != nil {
		return nil, err
	}

	view := newBookingView(*booking)
	return &view, nil
}

// ListForCustomer returns a customer's booking history.
func (s *Service) ListForCustomer(ctx context.Context, userID bson.ObjectID, page, limit int) ([]BookingView, int64, error) {
	bookings, total, err := s.bookings.ListForUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}
	return toBookingViews(bookings), total, nil
}

// ListBookings returns a filtered page for the admin dashboard.
func (s *Service) ListBookings(ctx context.Context, filter domain.BookingFilter, page, limit int) ([]BookingView, int64, error) {
	bookings, total, err := s.bookings.List(ctx, filter, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}
	return toBookingViews(bookings), total, nil
}

// Cancel releases a booking's slot at the customer's request.
func (s *Service) Cancel(ctx context.Context, id string, viewer Viewer) (*BookingView, error) {
	booking, err := s.loadBooking(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorize(booking, viewer); err != nil {
		return nil, err
	}

	if !booking.IsCancellable() {
		return nil, domain.ErrNotCancellable
	}

	if _, err := s.bookings.UpdateStatus(ctx, booking.ID, domain.StatusCancelled, true); err != nil {
		return nil, apperror.From(err)
	}

	booking.Status = domain.StatusCancelled
	s.emit(ctx, EventBookingCancelled, booking)

	logger.FromContext(ctx).Info("consultation cancelled",
		slog.String("module", "consultations"),
		slog.String("booking_id", booking.ID.Hex()),
	)

	return s.GetBooking(ctx, id, viewer)
}

// UpdateBookingStatus moves a booking through its lifecycle.
func (s *Service) UpdateBookingStatus(ctx context.Context, id, status string, actor Actor) (*BookingView, error) {
	if err := domain.ValidateStatus(status); err != nil {
		return nil, err
	}

	booking, err := s.loadBooking(ctx, id)
	if err != nil {
		return nil, err
	}

	// Cancelling or marking a no-show frees the slot; the other statuses keep
	// it held so the time stays blocked.
	if _, err := s.bookings.UpdateStatus(ctx, booking.ID, status, domain.ReleasesSlot(status)); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionBookingStatus, "consultation_booking", booking.ID.Hex(), map[string]any{
		"from": booking.Status,
		"to":   status,
	})

	previous := booking.Status
	booking.Status = status

	if domain.ReleasesSlot(status) {
		s.emit(ctx, EventBookingCancelled, booking)
	} else if previous != status {
		s.emit(ctx, EventBookingStatusChanged, booking)
	}

	return s.GetBooking(ctx, id, Viewer{IsAdmin: true})
}

// CreateType adds a consultation service.
func (s *Service) CreateType(ctx context.Context, input TypeInput, actor Actor) (*TypeView, error) {
	if input.Name == nil || input.DurationMinutes == nil || input.Price == nil {
		return nil, apperror.Validation.WithDetails(map[string]any{
			"name": "name, duration and price are required",
		})
	}

	name, err := validateTypeName(*input.Name)
	if err != nil {
		return nil, err
	}
	if err := validateDuration(*input.DurationMinutes); err != nil {
		return nil, err
	}
	if err := validatePrice(*input.Price); err != nil {
		return nil, err
	}

	existing, err := s.types.List(ctx, false)
	if err != nil {
		return nil, apperror.From(err)
	}

	now := time.Now().UTC()
	consultationType := &domain.Type{
		ID:              bson.NewObjectID(),
		Name:            name,
		DurationMinutes: *input.DurationMinutes,
		Price:           *input.Price,
		Currency:        domain.Currency,
		Active:          true,
		Position:        len(existing) + 1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if input.Description != nil {
		consultationType.Description = strings.TrimSpace(*input.Description)
	}
	if input.Active != nil {
		consultationType.Active = *input.Active
	}

	if err := s.types.Create(ctx, consultationType); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionTypeCreated, "consultation_type", consultationType.ID.Hex(), map[string]any{
		"name": consultationType.Name,
	})

	view := newTypeView(*consultationType)
	return &view, nil
}

// UpdateType applies a partial change to a consultation service.
func (s *Service) UpdateType(ctx context.Context, id string, input TypeInput, actor Actor) (*TypeView, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrTypeNotFound
	}

	fields := bson.M{}

	if input.Name != nil {
		name, err := validateTypeName(*input.Name)
		if err != nil {
			return nil, err
		}
		fields["name"] = name
	}
	if input.Description != nil {
		fields["description"] = strings.TrimSpace(*input.Description)
	}
	if input.DurationMinutes != nil {
		if err := validateDuration(*input.DurationMinutes); err != nil {
			return nil, err
		}
		fields["duration_minutes"] = *input.DurationMinutes
	}
	if input.Price != nil {
		if err := validatePrice(*input.Price); err != nil {
			return nil, err
		}
		fields["price"] = *input.Price
	}
	if input.Active != nil {
		fields["active"] = *input.Active
	}
	if input.Position != nil {
		fields["position"] = *input.Position
	}

	if err := s.types.Update(ctx, objectID, fields); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionTypeUpdated, "consultation_type", id, nil)

	updated, err := s.types.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	view := newTypeView(*updated)
	return &view, nil
}

// DeleteType removes a consultation service.
//
// A type with bookings is refused rather than deleted, because those bookings
// name it and would otherwise lose their meaning; deactivating it instead
// takes it off the booking page while keeping the history intact.
func (s *Service) DeleteType(ctx context.Context, id string, actor Actor) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return domain.ErrTypeNotFound
	}

	count, err := s.bookings.CountForType(ctx, objectID)
	if err != nil {
		return apperror.From(err)
	}
	if count > 0 {
		return domain.ErrTypeInUse.WithDetails(map[string]any{"bookings": count})
	}

	if err := s.types.Delete(ctx, objectID); err != nil {
		return apperror.From(err)
	}

	s.record(ctx, actor, actionTypeDeleted, "consultation_type", id, nil)
	return nil
}

// AddSlots opens dates and times for booking.
//
// Omitting times reuses the times already on offer, which is what the
// availability editor does when an administrator selects dates on the
// calendar; omitting dates applies new times to every date already open.
func (s *Service) AddSlots(ctx context.Context, input SlotsInput, actor Actor) (*AvailabilityView, error) {
	dates, times, err := s.resolveSlotInput(ctx, input, true)
	if err != nil {
		return nil, err
	}

	if _, err := s.slots.Add(ctx, dates, times); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionSlotsChanged, "consultation_slots", "", map[string]any{
		"action": "add", "dates": len(dates), "times": len(times),
	})

	return s.Availability(ctx, "")
}

// RemoveSlots closes dates or times for booking. Existing bookings are left
// alone: the grid controls what can still be booked, not what already was.
func (s *Service) RemoveSlots(ctx context.Context, input SlotsInput, actor Actor) (*AvailabilityView, error) {
	dates, times, err := s.resolveSlotInput(ctx, input, false)
	if err != nil {
		return nil, err
	}
	if len(dates) == 0 && len(times) == 0 {
		return nil, apperror.Validation.WithDetails(map[string]any{
			"dates": "give at least one date or time to remove",
		})
	}

	if _, err := s.slots.Remove(ctx, dates, times); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionSlotsChanged, "consultation_slots", "", map[string]any{
		"action": "remove", "dates": len(dates), "times": len(times),
	})

	return s.Availability(ctx, "")
}

// DeleteSlot removes one slot by identifier.
func (s *Service) DeleteSlot(ctx context.Context, id string, actor Actor) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperror.NotFound
	}

	if err := s.slots.DeleteByID(ctx, objectID); err != nil {
		return apperror.From(err)
	}

	s.record(ctx, actor, actionSlotsChanged, "consultation_slots", id, map[string]any{"action": "delete"})
	return nil
}

// resolveSlotInput validates the dates and times, filling in defaults for the
// side the caller left empty.
func (s *Service) resolveSlotInput(ctx context.Context, input SlotsInput, fillDefaults bool) ([]string, []string, error) {
	dates := make([]string, 0, len(input.Dates))
	for _, raw := range input.Dates {
		date, err := domain.ValidateDate(raw)
		if err != nil {
			return nil, nil, err
		}
		dates = append(dates, date)
	}

	times := make([]string, 0, len(input.Times))
	for _, raw := range input.Times {
		slotTime, err := domain.NormalizeTime(raw)
		if err != nil {
			return nil, nil, err
		}
		times = append(times, slotTime)
	}

	if !fillDefaults {
		return dates, times, nil
	}

	if len(times) == 0 {
		existing, err := s.slots.DistinctTimes(ctx)
		if err != nil {
			return nil, nil, apperror.From(err)
		}
		if len(existing) == 0 {
			existing = domain.DefaultTimes()
		}
		times = existing
	}

	if len(dates) == 0 {
		// Applying new times to the dates already open.
		slots, err := s.slots.List(ctx, domain.TodayString(), "")
		if err != nil {
			return nil, nil, apperror.From(err)
		}
		seen := map[string]bool{}
		for _, slot := range slots {
			if !seen[slot.Date] {
				seen[slot.Date] = true
				dates = append(dates, slot.Date)
			}
		}
	}

	return dates, times, nil
}

// EnsureDefaultAvailability opens the prototype's default grid when no slots
// exist, so a fresh deployment can take bookings immediately.
func (s *Service) EnsureDefaultAvailability(ctx context.Context) (int, error) {
	existing, err := s.slots.List(ctx, domain.TodayString(), "")
	if err != nil {
		return 0, apperror.From(err)
	}
	if len(existing) > 0 {
		return 0, nil
	}

	today := domain.Today()
	dates := make([]string, 0, domain.BookingDaysAhead)
	for day := 0; day < domain.BookingDaysAhead; day++ {
		dates = append(dates, today.AddDate(0, 0, day).Format("2006-01-02"))
	}

	return s.slots.Add(ctx, dates, domain.DefaultTimes())
}

// PurgePastSlots drops slots whose date has gone by.
func (s *Service) PurgePastSlots(ctx context.Context) (int64, error) {
	return s.slots.PurgeBefore(ctx, domain.TodayString())
}

// Stats returns the booking figures for the admin dashboard.
func (s *Service) Stats(ctx context.Context) (int64, error) {
	return s.bookings.Count(ctx)
}

// RecentBookings returns the newest bookings for the admin dashboard.
func (s *Service) RecentBookings(ctx context.Context, limit int) ([]BookingView, error) {
	bookings, err := s.bookings.Recent(ctx, limit)
	if err != nil {
		return nil, apperror.From(err)
	}
	return toBookingViews(bookings), nil
}

// SeedTypes installs the prototype's consultation services.
func (s *Service) SeedTypes(ctx context.Context) (int, error) {
	seeded := 0
	for _, seedType := range domain.SeedTypes() {
		record := seedType
		if err := s.types.Upsert(ctx, &record); err != nil {
			return seeded, apperror.From(err)
		}
		seeded++
	}
	return seeded, nil
}

// loadBooking fetches a booking by its hex identifier.
func (s *Service) loadBooking(ctx context.Context, id string) (*domain.Booking, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrBookingNotFound
	}

	booking, err := s.bookings.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	return booking, nil
}

// newReference mints a booking reference in the prototype's format: "CB-"
// followed by five digits.
func (s *Service) newReference(ctx context.Context) (string, error) {
	for attempt := 0; attempt < referenceAttempts; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(90_000))
		if err != nil {
			return "", fmt.Errorf("consultations: generate reference: %w", err)
		}

		candidate := fmt.Sprintf("CB-%05d", n.Int64()+10_000)

		taken, err := s.bookings.ReferenceExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("consultations: could not allocate a unique reference")
}

// emit records a domain event for the worker to act on.
func (s *Service) emit(ctx context.Context, eventType string, booking *domain.Booking) {
	if s.events == nil {
		return
	}

	payload := map[string]any{
		"booking_id":     booking.ID.Hex(),
		"reference":      booking.Reference,
		"type_name":      booking.TypeName,
		"date":           booking.Date,
		"time":           booking.Time,
		"name":           booking.Name,
		"email":          booking.Email,
		"phone":          booking.Phone,
		"status":         booking.Status,
		"payment_status": booking.PaymentStatus,
		"amount":         booking.TypePrice,
	}

	if err := s.events.Append(ctx, eventType, booking.ID.Hex(), payload); err != nil {
		logger.FromContext(ctx).Error("could not append booking event",
			slog.String("event_type", eventType),
			slog.String("booking_id", booking.ID.Hex()),
			slog.String("error", err.Error()),
		)
	}
}

// record writes an audit entry for an administrative change.
func (s *Service) record(ctx context.Context, actor Actor, action, resource, resourceID string, metadata map[string]any) {
	if s.auditor == nil || actor.ID.IsZero() {
		return
	}

	id := actor.ID
	s.auditor.Record(ctx, audit.Entry{
		ActorID:    &id,
		ActorEmail: actor.Email,
		ActorRole:  actor.Role,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Metadata:   metadata,
	})
}

// Viewer is the caller asking to see or change a booking.
type Viewer struct {
	UserID  bson.ObjectID
	IsAdmin bool
}

// NewViewer builds a viewer from an authenticated principal.
func NewViewer(userID bson.ObjectID, role string) Viewer {
	return Viewer{
		UserID:  userID,
		IsAdmin: role == usersdomain.RoleAdmin || role == usersdomain.RoleSuperAdmin,
	}
}

// authorize enforces ownership. A customer asking for someone else's booking
// gets "not found" rather than "forbidden", so identifiers cannot be probed.
func authorize(booking *domain.Booking, viewer Viewer) error {
	if viewer.IsAdmin {
		return nil
	}
	if booking.UserID != nil && *booking.UserID == viewer.UserID {
		return nil
	}
	return domain.ErrBookingNotFound
}

// authorizeWithReference is authorize plus the guest case: a booking with no
// account is reachable by presenting its reference alongside its identifier.
func authorizeWithReference(booking *domain.Booking, viewer Viewer, reference string) error {
	if booking.UserID == nil && !viewer.IsAdmin {
		supplied := strings.TrimSpace(reference)
		if supplied != "" && strings.EqualFold(supplied, booking.Reference) {
			return nil
		}
		return domain.ErrBookingNotFound
	}

	return authorize(booking, viewer)
}

// validateTypeName trims and bounds a consultation service name.
func validateTypeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "is required"})
	}
	if len(trimmed) > 120 {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "must be 120 characters or fewer"})
	}
	return trimmed, nil
}

// validateDuration bounds a consultation length. The admin form steps in
// quarter hours from 15 minutes.
func validateDuration(minutes int) error {
	if minutes < 15 || minutes > 480 {
		return apperror.Validation.WithDetails(map[string]any{
			"duration_minutes": "must be between 15 and 480 minutes",
		})
	}
	return nil
}

// validatePrice bounds a consultation fee in kobo.
func validatePrice(price int64) error {
	if price < 0 {
		return apperror.Validation.WithDetails(map[string]any{"price": "cannot be negative"})
	}
	if price > 10_000_000_000 {
		return apperror.Validation.WithDetails(map[string]any{
			"price": "is unrealistically large; amounts are in kobo",
		})
	}
	return nil
}
