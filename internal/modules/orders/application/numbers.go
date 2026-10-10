package application

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
)

// orderNumberAttempts bounds the retry loop for a unique order number.
const orderNumberAttempts = 10

// newOrderNumber returns a customer-facing order number in the prototype's
// format: "DG-" followed by six digits.
//
// The digits are cryptographically random rather than sequential, so the
// number does not leak how many orders the farm has taken.
func newOrderNumber(ctx context.Context, exists func(context.Context, string) (bool, error)) (string, error) {
	for attempt := 0; attempt < orderNumberAttempts; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(900_000))
		if err != nil {
			return "", fmt.Errorf("orders: generate order number: %w", err)
		}

		candidate := fmt.Sprintf("DG-%06d", n.Int64()+100_000)

		taken, err := exists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("orders: could not allocate a unique order number")
}
