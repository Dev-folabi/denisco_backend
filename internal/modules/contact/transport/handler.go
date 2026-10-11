// Package transport exposes the contact module over HTTP.
package transport

import (
	"context"
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/modules/contact/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
)

// ContactSubmitter is the slice of the contact module the handler needs.
type ContactSubmitter interface {
	// Submit validates a message and records it for delivery.
	Submit(ctx context.Context, message *domain.Message) error
}

// Handler serves the contact endpoint.
type Handler struct {
	service ContactSubmitter
}

// NewHandler wires the handler to the contact module.
func NewHandler(service ContactSubmitter) *Handler {
	return &Handler{service: service}
}

// request is the contact form's payload. The names match the web form's
// fields exactly so neither side has to translate.
type request struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// Submit accepts one message.
//
// The response is the same whether or not the mail provider is reachable —
// that is the outbox's job — so a visitor is never asked to resend because of
// a fault on the farm's side.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	var body request
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	message := &domain.Message{
		Name:    body.Name,
		Email:   body.Email,
		Phone:   body.Phone,
		Subject: body.Subject,
		Body:    body.Message,
	}

	if err := h.service.Submit(r.Context(), message); err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, "Thank you for your message. We will get back to you shortly.", nil)
}
