package domain

import "testing"

func TestValidateDeliveryMethod(t *testing.T) {
	for _, method := range []string{DeliveryHome, DeliveryPickup} {
		if err := ValidateDeliveryMethod(method); err != nil {
			t.Errorf("ValidateDeliveryMethod(%q) returned error: %v", method, err)
		}
	}

	for _, method := range []string{"", "courier", "DELIVERY"} {
		if err := ValidateDeliveryMethod(method); err == nil {
			t.Errorf("ValidateDeliveryMethod(%q) accepted an unknown method", method)
		}
	}
}

func TestValidateAddress(t *testing.T) {
	got, err := ValidateAddress(DeliveryHome, "  12 Sample Close, Gwarimpa, Abuja  ")
	if err != nil {
		t.Fatalf("ValidateAddress() returned error: %v", err)
	}
	if got != "12 Sample Close, Gwarimpa, Abuja" {
		t.Errorf("ValidateAddress() = %q, want the trimmed address", got)
	}

	if _, err := ValidateAddress(DeliveryHome, "   "); err == nil {
		t.Error("ValidateAddress() allowed home delivery without an address")
	}

	// Pickup needs no address, and discards one that is supplied.
	if got, err := ValidateAddress(DeliveryPickup, "ignored"); err != nil || got != "" {
		t.Errorf("ValidateAddress(pickup) = (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestStatusTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{StatusPending, StatusProcessing, true},
		{StatusPending, StatusCancelled, true},
		{StatusPending, StatusCompleted, false},
		{StatusPending, StatusDispatched, false},
		{StatusProcessing, StatusDispatched, true},
		{StatusProcessing, StatusCancelled, true},
		{StatusDispatched, StatusCompleted, true},
		{StatusDispatched, StatusCancelled, false},
		{StatusCompleted, StatusProcessing, false},
		{StatusCancelled, StatusProcessing, false},
		// Re-applying the current status is a no-op, not an error.
		{StatusCompleted, StatusCompleted, true},
	}

	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestValidateStatus(t *testing.T) {
	for _, status := range []string{StatusPending, StatusProcessing, StatusDispatched, StatusCompleted, StatusCancelled} {
		if err := ValidateStatus(status); err != nil {
			t.Errorf("ValidateStatus(%q) returned error: %v", status, err)
		}
	}

	for _, status := range []string{"", "shipped", "delivered", "PENDING"} {
		if err := ValidateStatus(status); err == nil {
			t.Errorf("ValidateStatus(%q) accepted an unknown status", status)
		}
	}
}

func TestOrderStateHelpers(t *testing.T) {
	pending := &Order{Status: StatusPending, PaymentStatus: PaymentPending}
	if !pending.IsCancellable() || !pending.IsPayable() || pending.IsPaid() {
		t.Error("a pending, unpaid order should be cancellable and payable")
	}

	paid := &Order{Status: StatusPending, PaymentStatus: PaymentPaid}
	if paid.IsCancellable() {
		t.Error("a paid order must not be customer-cancellable; that is a refund decision")
	}
	if paid.IsPayable() {
		t.Error("a paid order must not accept another payment attempt")
	}
	if !paid.IsPaid() {
		t.Error("IsPaid() did not recognise a paid order")
	}

	cancelled := &Order{Status: StatusCancelled, PaymentStatus: PaymentPending}
	if cancelled.IsPayable() {
		t.Error("a cancelled order must not accept a payment attempt")
	}

	// A declined card leaves the order open: the customer may pay with
	// another one, and each attempt stays on record.
	declined := &Order{Status: StatusPending, PaymentStatus: PaymentFailed}
	if !declined.IsPayable() {
		t.Error("an order whose payment failed should accept another attempt")
	}

	refunded := &Order{Status: StatusPending, PaymentStatus: PaymentRefunded}
	if refunded.IsPayable() {
		t.Error("a refunded order must not accept another payment attempt")
	}
}
