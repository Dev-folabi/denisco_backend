// Package transport exposes consultations over HTTP: the public booking page,
// the customer's own bookings and the administrative management screens.
package transport

import (
	"net/http"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	"github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Handler serves the consultation endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the consultations module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Mount registers every customer-facing consultation route.
//
// They share one subtree so the router has a single owner for
// "/consultations", with authentication applied per route rather than per
// group: browsing and booking are public, while a customer's own booking
// history requires a session.
//
// Booking takes optional authentication rather than required: the prototype
// lets a visitor book without an account, and a signed-in customer's booking
// is attached to them so it shows up in their history.
func (h *Handler) Mount(r chi.Router, optionalAuth, authenticate func(http.Handler) http.Handler) {
	r.Route("/consultations", func(consult chi.Router) {
		consult.Get("/types", h.ListTypes)
		consult.Get("/slots", h.Availability)
		consult.Get("/availability", h.Availability)

		consult.With(optionalAuth).Post("/bookings", h.Book)

		consult.With(authenticate).Get("/bookings", h.ListMine)
		// Reading one booking takes optional authentication for the same
		// reason booking does: a guest reaches their confirmation page holding
		// the identifier and the reference, and nothing else.
		consult.With(optionalAuth).Get("/bookings/{id}", h.Get)
		consult.With(authenticate).Post("/bookings/{id}/cancel", h.Cancel)
	})
}

// MountAdmin registers the management routes. The caller has already applied
// authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/consultations", func(consult chi.Router) {
		consult.Get("/types", h.ListTypesAdmin)
		consult.Post("/types", h.CreateType)
		consult.Patch("/types/{id}", h.UpdateType)
		consult.Delete("/types/{id}", h.DeleteType)

		consult.Get("/slots", h.AvailabilityAdmin)
		consult.Post("/slots", h.AddSlots)
		consult.Post("/slots/remove", h.RemoveSlots)
		consult.Delete("/slots/{id}", h.DeleteSlot)

		consult.Get("/bookings", h.ListBookings)
		consult.Get("/bookings/{id}", h.GetAdmin)
		consult.Patch("/bookings/{id}/status", h.UpdateStatus)
	})
}

// Request bodies.
type bookRequest struct {
	TypeID string `json:"type_id"`
	Date   string `json:"date"`
	Time   string `json:"time"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Notes  string `json:"notes"`
}

type typeRequest struct {
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	DurationMinutes *int    `json:"duration_minutes"`
	Price           *int64  `json:"price"`
	Active          *bool   `json:"active"`
	Position        *int    `json:"position"`
}

type slotsRequest struct {
	Dates []string `json:"dates"`
	Times []string `json:"times"`
}

type statusRequest struct {
	Status string `json:"status"`
}

// ListTypes returns the consultation services on offer.
func (h *Handler) ListTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.service.ListTypes(r.Context(), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Consultation types retrieved successfully.", types)
}

// Availability returns the booking calendar, with per-type availability when a
// consultation type is supplied.
func (h *Handler) Availability(w http.ResponseWriter, r *http.Request) {
	typeID := strings.TrimSpace(r.URL.Query().Get("type_id"))

	availability, err := h.service.Availability(r.Context(), typeID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Availability retrieved successfully.", availability)
}

// Book reserves a slot.
func (h *Handler) Book(w http.ResponseWriter, r *http.Request) {
	var body bookRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	input := application.BookInput{
		TypeID: body.TypeID,
		Date:   body.Date,
		Time:   body.Time,
		Name:   body.Name,
		Email:  body.Email,
		Phone:  body.Phone,
		Notes:  body.Notes,
	}

	// Attach the booking to the account when the visitor is signed in.
	if principal, ok := middleware.PrincipalFromContext(r.Context()); ok {
		if id, err := bson.ObjectIDFromHex(principal.UserID); err == nil {
			input.UserID = &id
		}
	}

	booking, err := h.service.Book(r.Context(), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Consultation booked successfully.", booking)
}

// ListMine returns the customer's own bookings.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	userID, _, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	page, limit := httpx.PageParams(r)

	bookings, total, err := h.service.ListForCustomer(r.Context(), userID, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Bookings retrieved successfully.", bookings, httpx.NewMeta(page, limit, total))
}

// Get returns one of the customer's bookings.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	// A guest has no session, so a missing principal is not an error here;
	// the reference in the query string is what stands in for one.
	view, err := optionalViewer(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	booking, err := h.service.GetBookingWithReference(r.Context(),
		chi.URLParam(r, "id"),
		strings.TrimSpace(r.URL.Query().Get("ref")),
		view)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Booking retrieved successfully.", booking)
}

// Cancel releases a booking's slot.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	viewer, err := viewer(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	booking, err := h.service.Cancel(r.Context(), chi.URLParam(r, "id"), viewer)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Booking cancelled successfully.", booking)
}

// ListTypesAdmin returns every consultation service, inactive ones included.
func (h *Handler) ListTypesAdmin(w http.ResponseWriter, r *http.Request) {
	types, err := h.service.ListTypes(r.Context(), false)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Consultation types retrieved successfully.", types)
}

// CreateType adds a consultation service.
func (h *Handler) CreateType(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body typeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	created, err := h.service.CreateType(r.Context(), toTypeInput(body), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Consultation type created successfully.", created)
}

// UpdateType applies a partial change to a consultation service.
func (h *Handler) UpdateType(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body typeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	updated, err := h.service.UpdateType(r.Context(), chi.URLParam(r, "id"), toTypeInput(body), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Consultation type updated successfully.", updated)
}

// DeleteType removes a consultation service that has no bookings.
func (h *Handler) DeleteType(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.DeleteType(r.Context(), chi.URLParam(r, "id"), actor); err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Consultation type deleted successfully.", nil)
}

// AvailabilityAdmin returns the full booking grid for the availability editor.
func (h *Handler) AvailabilityAdmin(w http.ResponseWriter, r *http.Request) {
	availability, err := h.service.Availability(r.Context(), "")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Availability retrieved successfully.", availability)
}

// AddSlots opens dates and times for booking.
func (h *Handler) AddSlots(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body slotsRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	availability, err := h.service.AddSlots(r.Context(),
		application.SlotsInput{Dates: body.Dates, Times: body.Times}, actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Availability updated successfully.", availability)
}

// RemoveSlots closes dates or times for booking.
func (h *Handler) RemoveSlots(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body slotsRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	availability, err := h.service.RemoveSlots(r.Context(),
		application.SlotsInput{Dates: body.Dates, Times: body.Times}, actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Availability updated successfully.", availability)
}

// DeleteSlot removes one slot.
func (h *Handler) DeleteSlot(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.DeleteSlot(r.Context(), chi.URLParam(r, "id"), actor); err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Time slot removed successfully.", nil)
}

// ListBookings returns the filtered booking list for the dashboard.
func (h *Handler) ListBookings(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	filter := domain.BookingFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Date:   strings.TrimSpace(r.URL.Query().Get("date")),
		Search: httpx.SearchParam(r),
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("type_id")); raw != "" {
		id, err := bson.ObjectIDFromHex(raw)
		if err != nil {
			httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"type_id": "is not a valid identifier"}))
			return
		}
		filter.TypeID = &id
	}

	bookings, total, err := h.service.ListBookings(r.Context(), filter, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Bookings retrieved successfully.", bookings, httpx.NewMeta(page, limit, total))
}

// GetAdmin returns any booking.
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	booking, err := h.service.GetBooking(r.Context(), chi.URLParam(r, "id"), application.Viewer{IsAdmin: true})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Booking retrieved successfully.", booking)
}

// UpdateStatus moves a booking through its lifecycle.
func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body statusRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	booking, err := h.service.UpdateBookingStatus(r.Context(),
		chi.URLParam(r, "id"), strings.TrimSpace(body.Status), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Booking status updated successfully.", booking)
}

// toTypeInput converts the request body into the application input.
func toTypeInput(body typeRequest) application.TypeInput {
	return application.TypeInput{
		Name:            body.Name,
		Description:     body.Description,
		DurationMinutes: body.DurationMinutes,
		Price:           body.Price,
		Active:          body.Active,
		Position:        body.Position,
	}
}

// principal returns the authenticated caller's id and role.
func principal(r *http.Request) (bson.ObjectID, string, error) {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return bson.ObjectID{}, "", apperror.Unauthorized
	}

	id, err := bson.ObjectIDFromHex(p.UserID)
	if err != nil {
		return bson.ObjectID{}, "", apperror.Unauthorized
	}

	return id, p.Role, nil
}

// viewer builds the ownership context for a booking read or change.
func viewer(r *http.Request) (application.Viewer, error) {
	id, role, err := principal(r)
	if err != nil {
		return application.Viewer{}, err
	}
	return application.NewViewer(id, role), nil
}

// optionalViewer builds a viewer from a session when there is one, and an
// anonymous viewer when there is not.
func optionalViewer(r *http.Request) (application.Viewer, error) {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return application.Viewer{}, nil
	}

	id, err := bson.ObjectIDFromHex(p.UserID)
	if err != nil {
		return application.Viewer{}, apperror.Unauthorized
	}

	return application.NewViewer(id, p.Role), nil
}

// actor builds the audit identity of the signed-in administrator.
func actor(r *http.Request) (application.Actor, error) {
	id, role, err := principal(r)
	if err != nil {
		return application.Actor{}, err
	}
	return application.Actor{ID: id, Role: role}, nil
}
