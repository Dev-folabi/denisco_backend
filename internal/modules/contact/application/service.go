// Package application accepts messages sent through the site's contact form
// and hands them to the farm.
package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/contact/domain"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ContactModule emits an outbox event for every accepted message. The worker
// is what turns it into an email, so the same durability rules apply here as
// to a password reset: a message that has been accepted is not lost because
// the mail provider was briefly unreachable.
type Outbox interface {
	Append(ctx context.Context, eventType, aggregateID string, payload map[string]any) error
}

// Service accepts contact messages.
type Service struct {
	events Outbox
}

// NewService wires the module to the outbox.
func NewService(events Outbox) *Service {
	return &Service{events: events}
}

// Submit validates a message and records it for delivery to the farm.
//
// The address the message goes to is deliberately not a request parameter: it
// comes from configuration, so a caller cannot aim the form at an arbitrary
// recipient. Nothing here is stored beyond the outbox event, which is why
// there is nothing for an administrator to list or delete.
func (s *Service) Submit(ctx context.Context, message *domain.Message) error {
	message.Normalise()

	if err := message.Validate(); err != nil {
		return apperror.From(err)
	}

	if message.Submitted.IsZero() {
		message.Submitted = time.Now().UTC()
	}

	payload := map[string]any{
		"name":         message.Name,
		"email":        message.Email,
		"subject":      message.Subject,
		"message":      message.Body,
		"submitted_at": message.Submitted.UTC().Format(time.RFC3339),
	}
	if message.Phone != "" {
		payload["phone"] = message.Phone
	}

	if err := s.events.Append(ctx, outbox.TypeContactReceived, bson.NewObjectID().Hex(), payload); err != nil {
		logger.FromContext(ctx).Error("could not record contact message",
			slog.String("module", "contact"),
			slog.String("error", err.Error()),
		)
		return apperror.Internal
	}

	logger.FromContext(ctx).Info("contact message accepted",
		slog.String("module", "contact"),
		slog.String("subject", message.Subject),
	)

	return nil
}
