package jobs

import (
	"strings"
	"testing"

	"github.com/Dev-folabi/denisco_backend/internal/outbox"
)

func TestMoney(t *testing.T) {
	// Amounts are kobo; the site shows naira with thousands separators.
	cases := map[int64]string{
		0:          "₦0",
		650_000:    "₦6,500",
		1_300_000:  "₦13,000",
		85_000_000: "₦850,000",
		250_000:    "₦2,500",
		650_050:    "₦6,500.50",
	}

	for kobo, want := range cases {
		if got := Money(kobo); got != want {
			t.Errorf("Money(%d) = %q, want %q", kobo, got, want)
		}
	}
}

func TestRenderCoversEveryNotification(t *testing.T) {
	// Every event the plan requires an email for must produce one.
	events := []string{
		outbox.TypeUserRegistered,
		outbox.TypePasswordResetRequested,
		outbox.TypePasswordChanged,
		outbox.TypeOrderCreated,
		outbox.TypeOrderPaid,
		outbox.TypeOrderStatusChanged,
		outbox.TypeOrderCancelled,
		outbox.TypeOrderRefunded,
		outbox.TypeOrderPaymentFailed,
		outbox.TypeConsultationBooked,
		outbox.TypeConsultationCancelled,
		outbox.TypeConsultationStatus,
		outbox.TypeConsultationRefunded,
		outbox.TypeConsultationPaymentFailed,
	}

	data := map[string]any{
		"email":        "customer@example.com",
		"first_name":   "Ada",
		"name":         "Ada Obi",
		"order_number": "DG-123456",
		"total":        float64(900_000),
		"status":       "dispatched",
		"reference":    "CB-12345",
		"type_name":    "Poultry Health & Management Advisory",
		"date":         "2026-10-10",
		"time":         "09:00 AM",
		"amount":       float64(1_500_000),
		"reset_url":    "https://denisco.test/reset-password?token=abc",
	}

	for _, eventType := range events {
		message, ok := Render(eventType, data, "https://denisco.test")
		if !ok {
			t.Errorf("Render(%q) produced no email", eventType)
			continue
		}
		if message.to != "customer@example.com" {
			t.Errorf("Render(%q) recipient = %q", eventType, message.to)
		}
		if strings.TrimSpace(message.subject) == "" {
			t.Errorf("Render(%q) has an empty subject", eventType)
		}
		if !strings.Contains(message.html, "<html>") {
			t.Errorf("Render(%q) did not produce an HTML body", eventType)
		}
		if strings.TrimSpace(message.text) == "" {
			t.Errorf("Render(%q) has no plain-text alternative", eventType)
		}
	}
}

func TestRenderIgnoresUnknownEvents(t *testing.T) {
	if _, ok := Render("something.else", map[string]any{}, "https://denisco.test"); ok {
		t.Error("Render() produced an email for an event with no customer-facing message")
	}
}

func TestRenderEscapesUntrustedValues(t *testing.T) {
	// Names and notes come from customer input and land in an HTML body.
	message, ok := Render(outbox.TypeOrderCreated, map[string]any{
		"email":        "customer@example.com",
		"name":         `Ada <script>alert(1)</script>`,
		"order_number": `DG-1<img src=x onerror=alert(1)>`,
		"total":        float64(650_000),
	}, "https://denisco.test")
	if !ok {
		t.Fatal("Render() produced no email")
	}

	// The angle brackets are what make injected markup dangerous; the escaped
	// text may still read "onerror=" harmlessly, so the assertion is on the
	// brackets rather than on the substring.
	if strings.Contains(message.html, "<script>") || strings.Contains(message.html, "<img ") {
		t.Errorf("Render() left injected markup unescaped:\n%s", message.html)
	}
	if !strings.Contains(message.html, "&lt;script&gt;") {
		t.Error("Render() did not escape the injected tag into text")
	}
}

func TestRenderFormatsMoneyFromQueuePayload(t *testing.T) {
	// Totals arrive as JSON numbers through the queue, so they decode as
	// float64 rather than int64.
	message, ok := Render(outbox.TypeOrderPaid, map[string]any{
		"email":        "customer@example.com",
		"name":         "Ada",
		"order_number": "DG-123456",
		"total":        float64(900_000),
	}, "https://denisco.test")
	if !ok {
		t.Fatal("Render() produced no email")
	}

	if !strings.Contains(message.html, "₦9,000") {
		t.Errorf("Render() did not format the amount as naira:\n%s", message.html)
	}
}

func TestRenderUsesSiteURLForLinks(t *testing.T) {
	message, _ := Render(outbox.TypeUserRegistered, map[string]any{
		"email":      "customer@example.com",
		"first_name": "Ada",
	}, "https://denisco.test/")

	// The trailing slash must not produce a double slash in the link.
	if !strings.Contains(message.html, "https://denisco.test/shop") {
		t.Errorf("Render() did not build the shop link correctly:\n%s", message.html)
	}
	if strings.Contains(message.html, "denisco.test//") {
		t.Error("Render() produced a double slash in the link")
	}
}

func TestLatePaymentNoticeGoesToStaffNotTheCustomer(t *testing.T) {
	// The customer's address is on the event, as it is on every order event.
	// This one must not use it: nobody should be told their order is on its
	// way when the farm has yet to decide between restocking and refunding.
	message, ok := Render(outbox.TypeOrderPaymentUnreconciled, map[string]any{
		"email":        "customer@example.com",
		"name":         "Ada Obi",
		"order_number": "DG-123456",
		"total":        float64(1_950_000),
	}, "https://admin.denisco.test")
	if !ok {
		t.Fatal("Render() produced no email for a late payment")
	}

	if message.to != "" {
		t.Errorf("recipient = %q, want it left to the worker's admin address", message.to)
	}
	if !isStaffEvent(outbox.TypeOrderPaymentUnreconciled) {
		t.Error("the late-payment notice is not treated as a staff event")
	}
	if !strings.Contains(message.html, "DG-123456") || !strings.Contains(message.html, "₦19,500") {
		t.Errorf("the notice does not name the order and the amount:\n%s", message.html)
	}
}

func TestRefundAndDeclineNoticesAreTheCustomerOnly(t *testing.T) {
	// These used to be invisible: a refund went through the dashboard with no
	// word to the customer, and a declined card left the order looking stuck.
	// Both are addressed to the customer, and neither is copied to the farm —
	// unlike a booking, they need no action there.
	cases := map[string]map[string]any{
		outbox.TypeOrderRefunded: {
			"email":        "customer@example.com",
			"name":         "Ada Obi",
			"order_number": "DG-123456",
			"total":        float64(900_000),
		},
		outbox.TypeOrderPaymentFailed: {
			"email":        "customer@example.com",
			"name":         "Ada Obi",
			"order_number": "DG-123456",
			"total":        float64(900_000),
		},
		outbox.TypeConsultationRefunded: {
			"email":     "customer@example.com",
			"name":      "Ada Obi",
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"amount":    float64(1_500_000),
		},
		outbox.TypeConsultationPaymentFailed: {
			"email":     "customer@example.com",
			"name":      "Ada Obi",
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"date":      "2026-10-10",
			"time":      "09:00 AM",
			"amount":    float64(1_500_000),
		},
	}

	for eventType, data := range cases {
		message, ok := Render(eventType, data, "https://denisco.test")
		if !ok {
			t.Fatalf("Render(%q) produced no email", eventType)
		}
		if message.to != "customer@example.com" {
			t.Errorf("Render(%q) recipient = %q, want the customer", eventType, message.to)
		}
		if isStaffEvent(eventType) {
			t.Errorf("%q is treated as a staff event; it is a customer notice", eventType)
		}
		if isBookingEvent(eventType) {
			t.Errorf("%q would be copied to the farm; a fee notice is the customer's alone", eventType)
		}
	}
}

func TestBookingNoticesAreStillCopiedToTheFarm(t *testing.T) {
	// The carve-out for fee notices must not take the appointment notices with
	// it: staff still want to hear about a booking, a cancellation and a
	// status change.
	for _, eventType := range []string{
		outbox.TypeConsultationBooked,
		outbox.TypeConsultationCancelled,
		outbox.TypeConsultationStatus,
	} {
		if !isBookingEvent(eventType) {
			t.Errorf("%q would no longer be copied to the farm", eventType)
		}
	}
}
