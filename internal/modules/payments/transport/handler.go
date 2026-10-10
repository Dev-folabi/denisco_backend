// Package transport exposes payments over HTTP, including the Paystack
// webhook endpoint.
package transport

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/modules/payments/application"
	"github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/platform/payment"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// webhookBodyLimit caps the webhook body independently of the global limit.
const webhookBodyLimit = 1 << 20

// signatureHeader is the header Paystack signs its webhook bodies with.
const signatureHeader = "X-Paystack-Signature"

// Handler serves the payment endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the payments module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// MountCustomer registers the customer-facing payment routes.
//
// Authentication is applied per route rather than to the whole subtree:
// paying for a consultation booking is open to guests, because the
// consultation page is public, while a customer's own transaction history
// needs a session. The handlers enforce who may act on which attempt.
func (h *Handler) MountCustomer(r chi.Router, optionalAuth, authenticate func(http.Handler) http.Handler) {
	r.Route("/payments", func(payments chi.Router) {
		payments.With(optionalAuth).Post("/initialize", h.Initialize)
		payments.With(optionalAuth).Post("/verify", h.Verify)
		payments.With(authenticate).Get("/", h.List)
		payments.With(optionalAuth).Get("/{reference}", h.Get)
	})
}

// MountAdmin registers the transaction routes. The caller has already applied
// authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/payments", func(payments chi.Router) {
		payments.Get("/", h.ListAdmin)
		payments.Get("/{id}", h.GetAdmin)
		payments.Post("/{id}/refund", h.Refund)
	})
}

// MountWebhooks registers the provider callback. It is deliberately outside
// every authentication and CSRF guard: the request comes from Paystack, not a
// browser, and is authenticated by its signature instead.
func (h *Handler) MountWebhooks(r chi.Router) {
	r.Post("/webhooks/paystack", h.PaystackWebhook)
}

// initializeRequest names what is being paid for. Exactly one of the order or
// booking identifiers is expected.
type initializeRequest struct {
	OrderID   string `json:"order_id"`
	BookingID string `json:"booking_id"`
	// BookingReference is required alongside BookingID for a guest booking,
	// which has no account to check the caller against.
	BookingReference string `json:"booking_reference"`
}

type verifyRequest struct {
	Reference string `json:"reference"`
}

// Initialize starts a payment attempt and returns the checkout URL.
func (h *Handler) Initialize(w http.ResponseWriter, r *http.Request) {
	var body initializeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	orderID := strings.TrimSpace(body.OrderID)
	bookingID := strings.TrimSpace(body.BookingID)

	if (orderID == "") == (bookingID == "") {
		httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{
			"order_id": "either an order_id or a booking_id is required, not both",
		}))
		return
	}

	// A booking payment may come from a guest; an order always belongs to an
	// account, so paying for one still requires a session.
	userID, role, err := optionalPrincipal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if orderID != "" && userID.IsZero() {
		httpx.Error(w, r, apperror.Unauthorized)
		return
	}

	var result *application.InitializeView
	if bookingID != "" {
		result, err = h.service.InitializeBooking(r.Context(), bookingID,
			strings.TrimSpace(body.BookingReference), userID, isAdmin(role))
	} else {
		result, err = h.service.Initialize(r.Context(), orderID, userID, isAdmin(role))
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Payment initialized successfully.", result)
}

// Verify re-checks an attempt with the provider and applies the result.
//
// The caller need not be signed in: a guest who has just paid for a
// consultation lands back on the callback page holding only the reference.
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	userID, role, err := optionalPrincipal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body verifyRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.service.Verify(r.Context(), body.Reference, userID, isAdmin(role))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Payment verified successfully.", result)
}

// Get returns one payment by reference, for the callback page to poll.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, role, err := optionalPrincipal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.service.Get(r.Context(), chi.URLParam(r, "reference"), userID, isAdmin(role))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Payment retrieved successfully.", result)
}

// List returns the customer's own transaction history.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	page, limit := httpx.PageParams(r)

	payments, total, err := h.service.ListForCustomer(r.Context(), userID, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Transactions retrieved successfully.", payments, httpx.NewMeta(page, limit, total))
}

// ListAdmin returns the filtered transaction list for the dashboard.
func (h *Handler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	filter := domain.Filter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Search: httpx.SearchParam(r),
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("customer_id")); raw != "" {
		id, err := bson.ObjectIDFromHex(raw)
		if err != nil {
			httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"customer_id": "is not a valid identifier"}))
			return
		}
		filter.UserID = &id
	}

	payments, total, err := h.service.List(r.Context(), filter, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Transactions retrieved successfully.", payments, httpx.NewMeta(page, limit, total))
}

// GetAdmin returns one transaction.
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Transaction retrieved successfully.", result)
}

// Refund returns a successful payment to the customer.
func (h *Handler) Refund(w http.ResponseWriter, r *http.Request) {
	userID, role, err := principal(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.service.Refund(r.Context(), chi.URLParam(r, "id"), application.Actor{ID: userID, Role: role})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Payment refunded successfully.", result)
}

// PaystackWebhook applies a provider callback.
//
// The signature is checked against the raw bytes before anything is decoded,
// and the response is 200 for every request we accept — including ones that
// change nothing — because a non-2xx reply makes Paystack retry a delivery
// that has in fact been handled.
func (h *Handler) PaystackWebhook(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, webhookBodyLimit))
	if err != nil {
		httpx.Error(w, r, apperror.BadRequest.Wrap(err))
		return
	}

	if !h.service.VerifySignature(rawBody, r.Header.Get(signatureHeader)) {
		log.Warn("rejected a webhook with an invalid signature",
			slog.String("module", "payments"),
			slog.String("ip", httpx.ClientIP(r)),
		)
		httpx.Error(w, r, apperror.Unauthorized.WithDetails(map[string]any{
			"signature": "the webhook signature could not be verified",
		}))
		return
	}

	event, err := payment.ParseWebhook(rawBody)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.HandleWebhook(r.Context(), event); err != nil {
		// A genuine processing failure returns 5xx so Paystack retries.
		log.Error("webhook processing failed",
			slog.String("module", "payments"),
			slog.String("event", event.Event),
			slog.String("error", err.Error()),
		)
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Webhook received.", nil)
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

// optionalPrincipal returns the caller's id and role when there is a session,
// and a zero id when there is not. A session that is present but unreadable is
// an error rather than an anonymous caller.
func optionalPrincipal(r *http.Request) (bson.ObjectID, string, error) {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return bson.ObjectID{}, "", nil
	}

	id, err := bson.ObjectIDFromHex(p.UserID)
	if err != nil {
		return bson.ObjectID{}, "", apperror.Unauthorized
	}

	return id, p.Role, nil
}

// isAdmin reports whether a role may act on any customer's payment.
func isAdmin(role string) bool {
	return role == usersdomain.RoleAdmin || role == usersdomain.RoleSuperAdmin
}
