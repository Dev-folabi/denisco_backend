// Package transport exposes the administrative read models over HTTP.
package transport

import (
	"net/http"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/modules/admin/application"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/go-chi/chi/v5"
)

// Handler serves the dashboard, customer and audit-log endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the admin module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Mount registers the administrative routes. The caller has already applied
// authentication and the admin role check.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/dashboard/overview", h.Overview)
	// The prototype's dashboard links to /dashboard; both resolve here.
	r.Get("/dashboard", h.Overview)

	r.Get("/customers", h.ListCustomers)
	r.Get("/customers/{id}", h.Customer)

	r.Get("/audit-logs", h.AuditLogs)
}

// Overview returns the dashboard's stat cards, activity lists and sales chart.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.Overview(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Dashboard overview retrieved successfully.", overview)
}

// ListCustomers returns a page of customer accounts.
func (h *Handler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	customers, total, err := h.service.ListCustomers(r.Context(), search, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Customers retrieved successfully.", customers, httpx.NewMeta(page, limit, total))
}

// Customer returns one customer with their purchase summary and history.
func (h *Handler) Customer(w http.ResponseWriter, r *http.Request) {
	customer, err := h.service.Customer(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Customer retrieved successfully.", customer)
}

// AuditLogs returns a page of the administrative audit trail.
func (h *Handler) AuditLogs(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	entries, total, err := h.service.AuditLogs(r.Context(), page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Audit logs retrieved successfully.", entries, httpx.NewMeta(page, limit, total))
}
