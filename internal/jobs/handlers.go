package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/mail"
	"github.com/Dev-folabi/denisco_backend/internal/platform/queue"
	"github.com/hibiken/asynq"
)

// ReservationExpiry is how long an unpaid order holds its stock before the
// reservation is returned to the shop.
const ReservationExpiry = 45 * time.Minute

// OrderMaintenance is the slice of the order module the worker needs.
type OrderMaintenance interface {
	// ExpireStaleReservations releases stock held by orders that were never
	// paid for, returning how many it freed.
	ExpireStaleReservations(ctx context.Context, olderThan time.Duration) (int, error)
}

// SlotMaintenance is the slice of the consultation module the worker needs.
type SlotMaintenance interface {
	// PurgePastSlots drops availability for dates that have passed.
	PurgePastSlots(ctx context.Context) (int64, error)
}

// Handlers implement the worker's tasks.
type Handlers struct {
	sender  mail.Sender
	orders  OrderMaintenance
	slots   SlotMaintenance
	siteURL string
	// adminEmail receives a copy of booking notifications, as the plan
	// requires; empty disables the copy.
	adminEmail string
	log        *slog.Logger
}

// NewHandlers wires the worker's dependencies.
func NewHandlers(sender mail.Sender, orders OrderMaintenance, slots SlotMaintenance, siteURL, adminEmail string, log *slog.Logger) *Handlers {
	return &Handlers{
		sender:     sender,
		orders:     orders,
		slots:      slots,
		siteURL:    siteURL,
		adminEmail: strings.TrimSpace(adminEmail),
		log:        log,
	}
}

// Register attaches every handler to the Asynq mux.
func (h *Handlers) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(queue.TaskSendEmail, h.SendEmail)
	mux.HandleFunc(queue.TaskExpireReservations, h.ExpireReservations)
	mux.HandleFunc(queue.TaskPurgeSlots, h.PurgeSlots)
}

// SendEmail delivers one transactional email.
//
// A permanent provider rejection (a bad address, say) is swallowed with
// asynq.SkipRetry: retrying would never succeed and would only keep the task
// cycling. Everything else is returned so Asynq retries with backoff.
func (h *Handlers) SendEmail(ctx context.Context, task *asynq.Task) error {
	var payload queue.EmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("jobs: decode email payload: %w, %w", err, asynq.SkipRetry)
	}

	log := h.log.With(
		slog.String("job", queue.TaskSendEmail),
		slog.String("event_id", payload.EventID),
		slog.String("event_type", payload.EventType),
	)

	message, ok := Render(payload.EventType, payload.Data, h.siteURL)
	if !ok {
		log.Debug("event has no customer-facing email; nothing to send")
		return nil
	}

	if isStaffEvent(payload.EventType) {
		// Staff-only notices never go to the customer, whose address is the
		// one carried on the event.
		if h.adminEmail == "" {
			log.Warn("staff notice has nowhere to go: ADMIN_NOTIFICATION_EMAIL is not set",
				slog.String("subject", message.subject))
			return nil
		}
		message.to = h.adminEmail
	}

	if message.to == "" {
		message.to = payload.To
	}
	if message.to == "" {
		log.Warn("event has no recipient address; dropping")
		return nil
	}

	if err := h.deliver(ctx, message, log); err != nil {
		return err
	}

	// Bookings are also copied to the farm so staff see new appointments.
	if h.adminEmail != "" && isBookingEvent(payload.EventType) {
		copyMessage := message
		copyMessage.to = h.adminEmail
		copyMessage.subject = "[Farm] " + message.subject

		if err := h.deliver(ctx, copyMessage, log.With(slog.String("recipient", "admin"))); err != nil {
			// The customer's copy already went out; failing the task would
			// resend it, so the admin copy is logged and let go.
			log.Warn("could not send the farm's copy of the booking notice",
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// deliver sends one message, translating provider failures into retry
// decisions.
func (h *Handlers) deliver(ctx context.Context, message email, log *slog.Logger) error {
	if !h.sender.Configured() {
		// Local development without a provider account: the worker records
		// what it would have sent rather than failing the task. The unset
		// variables are named so this line is not mistaken for a delivery.
		log.Info("email not sent: no provider configured",
			slog.String("missing", strings.Join(h.sender.Missing(), ",")),
			slog.String("to", message.to),
			slog.String("subject", message.subject),
		)
		return nil
	}

	start := time.Now()
	err := h.sender.Send(ctx, message.toMessage())
	duration := time.Since(start)

	if err == nil {
		log.Info("email sent",
			slog.String("to", message.to),
			slog.String("subject", message.subject),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
		return nil
	}

	var deliveryErr *mail.DeliveryError
	if errors.As(err, &deliveryErr) && deliveryErr.Permanent() {
		log.Error("email permanently rejected; not retrying",
			slog.String("to", message.to),
			slog.Int("status", deliveryErr.StatusCode),
		)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}

	return fmt.Errorf("jobs: send email: %w", err)
}

// ExpireReservations returns stock held by orders that were never paid for.
func (h *Handlers) ExpireReservations(ctx context.Context, _ *asynq.Task) error {
	released, err := h.orders.ExpireStaleReservations(ctx, ReservationExpiry)
	if err != nil {
		return fmt.Errorf("jobs: expire reservations: %w", err)
	}

	if released > 0 {
		h.log.Info("expired stock reservations released",
			slog.String("job", queue.TaskExpireReservations),
			slog.Int("orders", released),
		)
	}

	return nil
}

// PurgeSlots drops consultation availability for dates that have passed.
func (h *Handlers) PurgeSlots(ctx context.Context, _ *asynq.Task) error {
	removed, err := h.slots.PurgePastSlots(ctx)
	if err != nil {
		return fmt.Errorf("jobs: purge slots: %w", err)
	}

	if removed > 0 {
		h.log.Info("past consultation slots removed",
			slog.String("job", queue.TaskPurgeSlots),
			slog.Int64("slots", removed),
		)
	}

	return nil
}

// isBookingEvent reports whether the farm should receive a copy.
//
// This is an explicit list rather than a `consultation.` prefix match: a
// refund or a declined fee is between the customer and their bank, and the
// farm has nothing to act on.
func isBookingEvent(eventType string) bool {
	switch eventType {
	case outbox.TypeConsultationBooked, outbox.TypeConsultationCancelled, outbox.TypeConsultationStatus:
		return true
	default:
		return false
	}
}

// isStaffEvent reports whether an event is addressed to the farm alone.
func isStaffEvent(eventType string) bool {
	switch eventType {
	case outbox.TypeOrderPaymentUnreconciled, outbox.TypeContactReceived:
		return true
	default:
		return false
	}
}
