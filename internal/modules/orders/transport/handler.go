// Package transport exposes orders over HTTP, for both the customer account
// area and the admin dashboard.
package transport

import (
	"net/http"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Handler serves the order endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the orders module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// MountCustomer registers the customer-facing order routes. The caller has
// already applied authentication.
func (h *Handler) MountCustomer(r chi.Router) {
	r.Route("/orders", func(orders chi.Router) {
		orders.Post("/", h.Checkout)
		orders.Get("/", h.List)
		orders.Get("/number/{number}", h.GetByNumber)
		orders.Get("/{id}", h.Get)
		orders.Post("/{id}/cancel", h.Cancel)
	})
}

// MountAdmin registers the order-management routes. The caller has already
// applied authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/orders", func(orders chi.Router) {
		orders.Get("/", h.ListAdmin)
		orders.Get("/{id}", h.GetAdmin)
		orders.Patch("/{id}/status", h.UpdateStatus)
	})
}

type checkoutRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	DeliveryMethod string `json:"delivery_method"`
	Address        string `json:"address"`
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

// Checkout turns the customer's cart into an order.
func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	userID, _, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body checkoutRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	order, err := h.service.Checkout(r.Context(), userID, application.CheckoutInput{
		Name:           body.Name,
		Email:          body.Email,
		Phone:          body.Phone,
		DeliveryMethod: body.DeliveryMethod,
		Address:        body.Address,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Order placed successfully.", order)
}

// List returns the customer's own order history.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	page, limit := httpx.PageParams(r)

	orders, total, err := h.service.ListForCustomer(r.Context(), userID, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Orders retrieved successfully.", orders, httpx.NewMeta(page, limit, total))
}

// Get returns one of the customer's orders.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	viewer, err := viewer(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	order, err := h.service.Get(r.Context(), chi.URLParam(r, "id"), viewer)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Order retrieved successfully.", order)
}

// GetByNumber returns an order by its customer-facing number, which is what
// the confirmation page has after a redirect.
func (h *Handler) GetByNumber(w http.ResponseWriter, r *http.Request) {
	viewer, err := viewer(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	order, err := h.service.GetByNumber(r.Context(), chi.URLParam(r, "number"), viewer)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Order retrieved successfully.", order)
}

// Cancel withdraws an unpaid order.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	viewer, err := viewer(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	order, err := h.service.Cancel(r.Context(), chi.URLParam(r, "id"), viewer)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Order cancelled successfully.", order)
}

// ListAdmin returns the filtered order list for the dashboard.
func (h *Handler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	filter := domain.Filter{
		Status:        strings.TrimSpace(r.URL.Query().Get("status")),
		PaymentStatus: strings.TrimSpace(r.URL.Query().Get("payment_status")),
		Search:        httpx.SearchParam(r),
	}

	// needs_reconciliation surfaces late payments awaiting a refund decision.
	if raw := strings.TrimSpace(r.URL.Query().Get("needs_reconciliation")); raw != "" {
		flagged := raw == "true" || raw == "1"
		filter.NeedsReconciliation = &flagged
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("customer_id")); raw != "" {
		id, err := bson.ObjectIDFromHex(raw)
		if err != nil {
			httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"customer_id": "is not a valid identifier"}))
			return
		}
		filter.UserID = &id
	}

	orders, total, err := h.service.List(r.Context(), filter, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Orders retrieved successfully.", orders, httpx.NewMeta(page, limit, total))
}

// GetAdmin returns any order.
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	order, err := h.service.Get(r.Context(), chi.URLParam(r, "id"), application.Viewer{IsAdmin: true})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Order retrieved successfully.", order)
}

// UpdateStatus moves an order through fulfillment.
func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	userID, role, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body updateStatusRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	order, err := h.service.UpdateStatus(r.Context(), chi.URLParam(r, "id"), strings.TrimSpace(body.Status), application.Actor{
		ID:   userID,
		Role: role,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Order status updated successfully.", order)
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

// viewer builds the ownership context for an order read or change.
func viewer(r *http.Request) (application.Viewer, error) {
	id, role, err := principal(r)
	if err != nil {
		return application.Viewer{}, err
	}
	return application.NewViewer(id, role), nil
}
