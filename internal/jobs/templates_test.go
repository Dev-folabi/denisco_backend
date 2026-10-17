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
	// Render has no copy of this notice at all — only the farm's version
	// exists, and it links to the dashboard, not to the customer's account.
	data := map[string]any{
		"email":        "customer@example.com",
		"name":         "Ada Obi",
		"order_number": "DG-123456",
		"total":        float64(1_950_000),
	}

	if _, ok := Render(outbox.TypeOrderPaymentUnreconciled, data, "https://denisco.test"); ok {
		t.Error("Render() produced a customer copy of the late-payment notice")
	}

	message, ok := RenderAdmin(outbox.TypeOrderPaymentUnreconciled, data, "https://admin.denisco.test/")
	if !ok {
		t.Fatal("RenderAdmin() produced no email for a late payment")
	}

	if message.to != "" {
		t.Errorf("recipient = %q, want it left to the worker's admin address", message.to)
	}
	if !strings.Contains(message.html, "https://admin.denisco.test/orders") {
		t.Errorf("the notice does not link to the dashboard:\n%s", message.html)
	}
	if strings.Contains(message.html, "https://denisco.test/") {
		t.Error("the notice links into the customer's site")
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
		if _, ok := RenderAdmin(eventType, data, "https://admin.denisco.test"); ok {
			t.Errorf("RenderAdmin(%q) produced a farm notice; a fee notice is the customer's alone", eventType)
		}
	}
}

func TestFarmNoticesCoverBookingsAndOrders(t *testing.T) {
	// Each of these events reaches the customer first; the farm then gets
	// its own view of the same news, written for staff and linked to the
	// dashboard rather than to the customer's account pages.
	cases := map[string]map[string]any{
		outbox.TypeConsultationBooked: {
			"reference":      "CB-12345",
			"type_name":      "Poultry Health & Management Advisory",
			"date":           "2026-10-20",
			"time":           "09:00 AM",
			"name":           "Ada Obi",
			"email":          "customer@example.com",
			"phone":          "0803 000 0000",
			"payment_status": "paid",
			"amount":         float64(1_000_000),
		},
		outbox.TypeConsultationCancelled: {
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"date":      "2026-10-20",
			"time":      "09:00 AM",
			"name":      "Ada Obi",
			"email":     "customer@example.com",
		},
		outbox.TypeConsultationStatus: {
			"reference": "CB-12345",
			"type_name": "Poultry Health & Management Advisory",
			"date":      "2026-10-20",
			"time":      "09:00 AM",
			"status":    "confirmed",
			"name":      "Ada Obi",
			"email":     "customer@example.com",
		},
		outbox.TypeOrderCreated: {
			"order_number": "DG-123456",
			"name":         "Ada Obi",
			"email":        "customer@example.com",
			"total":        float64(900_000),
			"status":       "pending",
		},
		outbox.TypeOrderPaid: {
			"order_number": "DG-123456",
			"name":         "Ada Obi",
			"email":        "customer@example.com",
			"total":        float64(900_000),
			"status":       "pending",
		},
	}

	for eventType, data := range cases {
		// The customer still hears about it, from their own address.
		customer, ok := Render(eventType, data, "https://denisco.test")
		if !ok {
			t.Fatalf("Render(%q) produced no email for the customer", eventType)
		}
		if customer.to != "customer@example.com" {
			t.Errorf("Render(%q) recipient = %q, want the customer", eventType, customer.to)
		}

		message, ok := RenderAdmin(eventType, data, "https://admin.denisco.test/")
		if !ok {
			t.Errorf("RenderAdmin(%q) produced no notice for the farm", eventType)
			continue
		}
		if message.to != "" {
			t.Errorf("RenderAdmin(%q) recipient = %q, want it left to the worker's admin address", eventType, message.to)
		}
		if strings.TrimSpace(message.subject) == "" {
			t.Errorf("RenderAdmin(%q) has an empty subject", eventType)
		}
		if !strings.Contains(message.html, "https://admin.denisco.test/") {
			t.Errorf("RenderAdmin(%q) does not link to the dashboard:\n%s", eventType, message.html)
		}
		if !strings.Contains(message.text, "customer@example.com") {
			t.Errorf("RenderAdmin(%q) does not name the customer for the farm:\n%s", eventType, message.text)
		}
	}
}

func TestStaffOnlyNoticesRenderForTheFarmAlone(t *testing.T) {
	// A website enquiry never concerned the customer, and a late payment
	// must not reach them: both exist only in the farm's form.
	cases := map[string]map[string]any{
		outbox.TypeContactReceived: {
			"name":    "A Visitor",
			"email":   "visitor@example.com",
			"subject": "Bulk order enquiry",
			"message": "Do you deliver to Ibadan?",
		},
		outbox.TypeOrderPaymentUnreconciled: {
			"order_number": "DG-123456",
			"total":        float64(1_950_000),
		},
	}

	for eventType, data := range cases {
		if _, ok := Render(eventType, data, "https://denisco.test"); ok {
			t.Errorf("Render(%q) produced a customer copy of a staff notice", eventType)
		}
		message, ok := RenderAdmin(eventType, data, "https://admin.denisco.test")
		if !ok {
			t.Errorf("RenderAdmin(%q) produced no notice for the farm", eventType)
			continue
		}
		if message.to != "" {
			t.Errorf("RenderAdmin(%q) recipient = %q, want it left to the worker's admin address", eventType, message.to)
		}
	}
}

func TestRenderAdminEscapesUntrustedValues(t *testing.T) {
	// The customer's name reaches the farm's inbox the same way it reaches
	// the customer's: already escaped.
	message, ok := RenderAdmin(outbox.TypeOrderCreated, map[string]any{
		"name":         `Ada <script>alert(1)</script>`,
		"email":        "customer@example.com",
		"order_number": "DG-123456",
		"total":        float64(650_000),
	}, "https://admin.denisco.test")
	if !ok {
		t.Fatal("RenderAdmin() produced no notice")
	}

	if strings.Contains(message.html, "<script>") {
		t.Errorf("RenderAdmin() left injected markup unescaped:\n%s", message.html)
	}
}
