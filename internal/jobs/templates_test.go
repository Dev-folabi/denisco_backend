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
		outbox.TypeConsultationBooked,
		outbox.TypeConsultationCancelled,
		outbox.TypeConsultationStatus,
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
