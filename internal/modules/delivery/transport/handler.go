// Package transport exposes home delivery over HTTP: the public offering
// the checkout reads, and the administrative surface behind it.
package transport

import (
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/modules/delivery/application"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Handler serves the delivery endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the delivery module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Mount registers the public delivery route.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/delivery", h.Options)
}

// MountAdmin registers the delivery-management routes. The caller has
// already applied authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/delivery", func(delivery chi.Router) {
		delivery.Get("/", h.Admin)
		delivery.Patch("/", h.SetEnabled)

		delivery.Route("/locations", func(locations chi.Router) {
			locations.Post("/", h.CreateLocation)
			locations.Patch("/{id}", h.UpdateLocation)
			locations.Delete("/{id}", h.DeleteLocation)
		})
	})
}

type locationRequest struct {
	Name   *string `json:"name"`
	Fee    *int64  `json:"fee"`
	Active *bool   `json:"active"`
}

type setEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

// Options returns the customer-facing offering.
func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	offering, err := h.service.Options(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Delivery options retrieved successfully.", offering)
}

// Admin returns the master switch and every location.
func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	offering, err := h.service.Admin(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Delivery settings retrieved successfully.", offering)
}

// SetEnabled switches home delivery on or off.
func (h *Handler) SetEnabled(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body setEnabledRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	offering, err := h.service.SetEnabled(r.Context(), application.SetEnabledInput{Enabled: body.Enabled}, actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Home delivery setting updated successfully.", offering)
}

// CreateLocation adds a delivery area.
func (h *Handler) CreateLocation(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body locationRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	created, err := h.service.CreateLocation(r.Context(), toLocationInput(body), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Delivery location created successfully.", created)
}

// UpdateLocation changes a delivery area.
func (h *Handler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body locationRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	updated, err := h.service.UpdateLocation(r.Context(), chi.URLParam(r, "id"), toLocationInput(body), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Delivery location updated successfully.", updated)
}

// DeleteLocation removes a delivery area.
func (h *Handler) DeleteLocation(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.DeleteLocation(r.Context(), chi.URLParam(r, "id"), actor); err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.NoContent(w)
}

func toLocationInput(body locationRequest) application.LocationInput {
	return application.LocationInput{
		Name:   body.Name,
		Fee:    body.Fee,
		Active: body.Active,
	}
}

// actor identifies the administrator behind a change.
func actor(r *http.Request) (application.Actor, error) {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return application.Actor{}, apperror.Unauthorized
	}

	id, err := bson.ObjectIDFromHex(p.UserID)
	if err != nil {
		return application.Actor{}, apperror.Unauthorized
	}

	return application.Actor{ID: id, Role: p.Role}, nil
}
