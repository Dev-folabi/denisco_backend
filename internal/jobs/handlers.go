package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	sender mail.Sender
	orders OrderMaintenance
	slots  SlotMaintenance
	// siteURL is the customer site; adminURL the dashboard. Notices that
	// name a link point at whichever audience they are written for.
	siteURL  string
	adminURL string
	// adminEmail receives every farm notice; empty disables them.
	adminEmail string
	log        *slog.Logger
}

// NewHandlers wires the worker's dependencies.
func NewHandlers(sender mail.Sender, orders OrderMaintenance, slots SlotMaintenance, siteURL, adminURL, adminEmail string, log *slog.Logger) *Handlers {
	return &Handlers{
		sender:     sender,
		orders:     orders,
		slots:      slots,
		siteURL:    strings.TrimRight(siteURL, "/"),
		adminURL:   strings.TrimRight(adminURL, "/"),
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

// SendEmail delivers one transactional email — the customer's copy, the
// farm's notice, or for some events only one of the two.
//
// A permanent provider rejection (a bad address, say) is swallowed with
// asynq.SkipRetry: retrying would never succeed and would only keep the task
// cycling. Everything else is returned so Asynq retries with backoff. The
// farm's notice is the exception once the customer's copy has gone: failing
// the task then would resend the customer's mail, so the notice is logged
// and let go. When it is the only copy, it is retried like any other mail.
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

	message, hasCustomer := Render(payload.EventType, payload.Data, h.siteURL)
	farmNotice, hasFarm := RenderAdmin(payload.EventType, payload.Data, h.adminURL)

	if !hasCustomer && !hasFarm {
		log.Debug("event has no email; nothing to send")
		return nil
	}

	// The customer's copy comes first: if the provider refuses it, the task
	// is retried and the farm's notice waits, rather than the farm hearing
	// about an order the customer was never told of.
	sentToCustomer := false
	if hasCustomer {
		if message.to == "" {
			message.to = payload.To
		}
		if message.to == "" {
			log.Warn("event has no recipient address; skipping the customer's copy")
		} else if err := h.deliver(ctx, message, log); err != nil {
			return err
		} else {
			sentToCustomer = true
		}
	}

	if !hasFarm {
		return nil
	}
	if h.adminEmail == "" {
		log.Warn("notice has nowhere to go: ADMIN_NOTIFICATION_EMAIL is not set",
			slog.String("subject", farmNotice.subject))
		return nil
	}

	farmNotice.to = h.adminEmail
	if err := h.deliver(ctx, farmNotice, log.With(slog.String("recipient", "admin"))); err != nil {
		if !sentToCustomer {
			return err
		}
		log.Warn("could not send the farm's notice",
			slog.String("error", err.Error()))
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
