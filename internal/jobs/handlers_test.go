package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/mail"
	"github.com/Dev-folabi/denisco_backend/internal/platform/queue"
)

// recordingSender captures every message instead of delivering it.
type recordingSender struct {
	sent []mail.Message
}

func (r *recordingSender) Send(_ context.Context, message mail.Message) error {
	r.sent = append(r.sent, message)
	return nil
}

func (r *recordingSender) Configured() bool  { return true }
func (r *recordingSender) Missing() []string { return nil }

// send runs one email task through the handler and returns what was sent.
func send(t *testing.T, sender mail.Sender, eventType string, data map[string]any) []mail.Message {
	t.Helper()

	task, err := queue.NewEmailTask(queue.EmailPayload{
		EventID:   "evt_test",
		EventType: eventType,
		To:        "customer@example.com",
		Data:      data,
	})
	if err != nil {
		t.Fatalf("build task: %v", err)
	}

	handlers := NewHandlers(sender, nil, nil,
		"https://denisco.test", "https://admin.denisco.test",
		"farm@example.com",
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := handlers.SendEmail(context.Background(), task); err != nil {
		t.Fatalf("SendEmail(%q) = %v", eventType, err)
	}

	if recorder, ok := sender.(*recordingSender); ok {
		return recorder.sent
	}
	return nil
}

func TestSendEmailSendsBothSidesForABooking(t *testing.T) {
	// A confirmation goes to the person who booked, and the farm gets its
	// own notice of the same appointment — two messages, one task.
	sender := &recordingSender{}
	sent := send(t, sender, outbox.TypeConsultationBooked, map[string]any{
		"reference":      "CB-12345",
		"type_name":      "Poultry Health & Management Advisory",
		"date":           "2026-10-20",
		"time":           "09:00 AM",
		"name":           "Ada Obi",
		"email":          "customer@example.com",
		"phone":          "0803 000 0000",
		"payment_status": "paid",
		"amount":         float64(1_000_000),
	})

	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2: %+v", len(sent), sent)
	}

	if sent[0].To != "customer@example.com" {
		t.Errorf("first message to %q, want the booker", sent[0].To)
	}
	if sent[0].Subject != "Your consultation is booked — CB-12345" {
		t.Errorf("first message subject = %q", sent[0].Subject)
	}

	if sent[1].To != "farm@example.com" {
		t.Errorf("second message to %q, want the farm", sent[1].To)
	}
	if sent[1].Subject != "New consultation booked — CB-12345" {
		t.Errorf("second message subject = %q", sent[1].Subject)
	}
}

func TestSendEmailSendsTheFarmANoticeForAnOrder(t *testing.T) {
	// Orders previously reached the customer only; the farm now hears
	// about a new order and about its payment landing.
	for _, eventType := range []string{outbox.TypeOrderCreated, outbox.TypeOrderPaid} {
		sender := &recordingSender{}
		sent := send(t, sender, eventType, map[string]any{
			"order_number": "DG-123456",
			"name":         "Ada Obi",
			"email":        "customer@example.com",
			"total":        float64(900_000),
			"status":       "pending",
		})

		if len(sent) != 2 {
			t.Fatalf("%s: sent %d messages, want 2", eventType, len(sent))
		}
		if sent[0].To != "customer@example.com" {
			t.Errorf("%s: first message to %q, want the customer", eventType, sent[0].To)
		}
		if sent[1].To != "farm@example.com" {
			t.Errorf("%s: second message to %q, want the farm", eventType, sent[1].To)
		}
	}
}

func TestSendEmailKeepsRefundsAndDeclinesFromTheFarm(t *testing.T) {
	// A refund or a declined card is between the customer and their bank;
	// the farm has nothing to act on, so exactly one message leaves.
	for _, eventType := range []string{
		outbox.TypeOrderRefunded,
		outbox.TypeOrderPaymentFailed,
		outbox.TypeConsultationRefunded,
		outbox.TypeConsultationPaymentFailed,
	} {
		sender := &recordingSender{}
		sent := send(t, sender, eventType, map[string]any{
			"reference":    "CB-12345",
			"order_number": "DG-123456",
			"name":         "Ada Obi",
			"type_name":    "Poultry Health & Management Advisory",
			"total":        float64(900_000),
			"amount":       float64(1_000_000),
		})

		if len(sent) != 1 {
			t.Fatalf("%s: sent %d messages, want 1", eventType, len(sent))
		}
		if sent[0].To != "customer@example.com" {
			t.Errorf("%s: message to %q, want the customer", eventType, sent[0].To)
		}
	}
}

func TestSendEmailGivesStaffOnlyNoticesToTheFarm(t *testing.T) {
	// A website enquiry and a late payment have no customer copy, so the
	// single message goes to the farm.
	for _, eventType := range []string{
		outbox.TypeContactReceived,
		outbox.TypeOrderPaymentUnreconciled,
	} {
		sender := &recordingSender{}
		sent := send(t, sender, eventType, map[string]any{
			"name":         "A Visitor",
			"email":        "visitor@example.com",
			"subject":      "Bulk order enquiry",
			"message":      "Do you deliver to Ibadan?",
			"order_number": "DG-123456",
			"total":        float64(1_950_000),
		})

		if len(sent) != 1 {
			t.Fatalf("%s: sent %d messages, want 1", eventType, len(sent))
		}
		if sent[0].To != "farm@example.com" {
			t.Errorf("%s: message to %q, want the farm", eventType, sent[0].To)
		}
	}
}

func TestSendEmailStillNotifiesTheFarmWithoutACustomerAddress(t *testing.T) {
	// A booking whose address is missing on both the event and the payload
	// cannot be confirmed to anyone — but the appointment exists, and the
	// farm must still hear about it.
	sender := &recordingSender{}

	task, err := queue.NewEmailTask(queue.EmailPayload{
		EventID:   "evt_test",
		EventType: outbox.TypeConsultationBooked,
		To:        "", // no address anywhere
		Data: map[string]any{
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"date":      "2026-10-20",
			"time":      "09:00 AM",
			"name":      "",
			"email":     "",
		},
	})
	if err != nil {
		t.Fatalf("build task: %v", err)
	}

	handlers := NewHandlers(sender, nil, nil,
		"https://denisco.test", "https://admin.denisco.test",
		"farm@example.com",
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := handlers.SendEmail(context.Background(), task); err != nil {
		t.Fatalf("SendEmail() = %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d messages, want only the farm's", len(sender.sent))
	}
	if sender.sent[0].To != "farm@example.com" {
		t.Errorf("message to %q, want the farm", sender.sent[0].To)
	}
}

func TestSendEmailIgnoresEventsWithNoMail(t *testing.T) {
	sender := &recordingSender{}

	task, err := queue.NewEmailTask(queue.EmailPayload{
		EventID:   "evt_test",
		EventType: "cart.updated",
		Data:      map[string]any{},
	})
	if err != nil {
		t.Fatalf("build task: %v", err)
	}

	handlers := NewHandlers(sender, nil, nil,
		"https://denisco.test", "https://admin.denisco.test",
		"farm@example.com",
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := handlers.SendEmail(context.Background(), task); err != nil {
		t.Fatalf("SendEmail() = %v", err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("sent %d messages for an event with no mail, want 0", len(sender.sent))
	}
}

// TestSendEmailSurvivesAMissingAdminAddress guards the configuration edge:
// with no ADMIN_NOTIFICATION_EMAIL the customer's copy still goes out and
// the farm's notice is skipped rather than failing the task.
func TestSendEmailSurvivesAMissingAdminAddress(t *testing.T) {
	sender := &recordingSender{}

	task, err := queue.NewEmailTask(queue.EmailPayload{
		EventID:   "evt_test",
		EventType: outbox.TypeConsultationBooked,
		Data: map[string]any{
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"date":      "2026-10-20",
			"time":      "09:00 AM",
			"name":      "Ada Obi",
			"email":     "customer@example.com",
		},
	})
	if err != nil {
		t.Fatalf("build task: %v", err)
	}

	handlers := NewHandlers(sender, nil, nil,
		"https://denisco.test", "https://admin.denisco.test",
		"", // ADMIN_NOTIFICATION_EMAIL unset
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := handlers.SendEmail(context.Background(), task); err != nil {
		t.Fatalf("SendEmail() = %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d messages, want only the customer's", len(sender.sent))
	}
	if sender.sent[0].To != "customer@example.com" {
		t.Errorf("message to %q, want the customer", sender.sent[0].To)
	}
}
