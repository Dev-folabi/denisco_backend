// Package transport exposes categories over HTTP.
package transport

import (
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/modules/categories/application"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/go-chi/chi/v5"
)

// Handler serves the category endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the categories module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Mount registers the public category routes.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/categories", h.List)
}

// List returns the active categories, in display order.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	categories, err := h.service.List(r.Context(), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Categories retrieved successfully.", categories)
}
