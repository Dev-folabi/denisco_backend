package apperror

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsMatchesAcrossClones(t *testing.T) {
	sentinel := New(http.StatusNotFound, "ORDER_NOT_FOUND", "The order could not be found.")

	// WithDetails and Wrap both return clones; callers must still be able to
	// recognise the condition they describe.
	withDetails := sentinel.WithDetails(map[string]any{"order_id": "abc"})
	wrapped := sentinel.Wrap(errors.New("mongo: no documents"))

	if !errors.Is(withDetails, sentinel) {
		t.Error("errors.Is() did not match a clone carrying details")
	}
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is() did not match a clone carrying a cause")
	}

	// A different condition must not match.
	other := New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "The product could not be found.")
	if errors.Is(withDetails, other) {
		t.Error("errors.Is() matched a different error code")
	}
}

func TestIsMatchesThroughWrapping(t *testing.T) {
	sentinel := New(http.StatusConflict, "INSUFFICIENT_STOCK", "Not enough stock.")
	wrapped := fmt.Errorf("checkout failed: %w", sentinel)

	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is() did not see through fmt.Errorf wrapping")
	}
}

func TestFromPreservesApplicationErrors(t *testing.T) {
	sentinel := New(http.StatusConflict, "CONFLICT", "Conflict.")

	if got := From(fmt.Errorf("wrapped: %w", sentinel)); got.Code != "CONFLICT" {
		t.Errorf("From() code = %q, want CONFLICT", got.Code)
	}
}

func TestFromHidesUnknownErrors(t *testing.T) {
	got := From(errors.New("mongo: auth failed for connection string"))

	if got.Code != "INTERNAL_ERROR" {
		t.Errorf("From() code = %q, want INTERNAL_ERROR", got.Code)
	}
	if got.Status != http.StatusInternalServerError {
		t.Errorf("From() status = %d, want 500", got.Status)
	}
	if got.Message == "mongo: auth failed for connection string" {
		t.Error("From() exposed the underlying cause as the client message")
	}
}

func TestDetailsAreNotSharedBetweenClones(t *testing.T) {
	sentinel := New(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid.")

	first := sentinel.WithDetails(map[string]any{"email": "is required"})
	second := sentinel.WithDetails(map[string]any{"phone": "is required"})

	if _, ok := first.Details["phone"]; ok {
		t.Error("one clone's details leaked into another")
	}
	if sentinel.Details != nil {
		t.Error("WithDetails() mutated the shared sentinel")
	}
	_ = second
}
