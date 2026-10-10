// Package transport exposes stock management over HTTP. Every route here is
// administrative: customers only ever see stock through the catalogue.
package transport

import (
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/inventory/application"
	"github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Handler serves the inventory endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the inventory module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// MountAdmin registers the stock routes. The caller has already applied
// authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/inventory", func(inventory chi.Router) {
		inventory.Get("/", h.List)
		inventory.Get("/movements", h.Movements)
		inventory.Post("/adjustments", h.Adjust)
	})
}

// adjustRequest is a manual stock correction.
type adjustRequest struct {
	ProductID string `json:"product_id"`
	// Quantity is a signed delta: positive adds stock, negative removes it.
	Quantity int    `json:"quantity"`
	Reason   string `json:"reason"`
}

// levelView is the admin projection of a stock record.
type levelView struct {
	ProductID    string `json:"product_id"`
	AvailableQty int    `json:"available_qty"`
	ReservedQty  int    `json:"reserved_qty"`
	Sellable     int    `json:"sellable"`
	StockBadge   string `json:"stock_badge"`
	UpdatedAt    string `json:"updated_at"`
}

// movementView is the admin projection of a stock movement.
type movementView struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
	Type      string `json:"type"`
	Quantity  int    `json:"quantity"`
	Reason    string `json:"reason,omitempty"`
	ActorID   string `json:"actor_id,omitempty"`
	Reference string `json:"reference,omitempty"`
	CreatedAt string `json:"created_at"`
}

// List returns the stock overview.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	levels, total, err := h.service.Overview(r.Context(), page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	views := make([]levelView, 0, len(levels))
	for _, level := range levels {
		views = append(views, newLevelView(level))
	}

	httpx.Paginated(w, "Inventory retrieved successfully.", views, httpx.NewMeta(page, limit, total))
}

// Movements returns the stock history, optionally for one product.
func (h *Handler) Movements(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	var productID *bson.ObjectID
	if raw := strings.TrimSpace(r.URL.Query().Get("product_id")); raw != "" {
		id, err := bson.ObjectIDFromHex(raw)
		if err != nil {
			httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"product_id": "is not a valid identifier"}))
			return
		}
		productID = &id
	}

	movements, total, err := h.service.Movements(r.Context(), productID, page, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	views := make([]movementView, 0, len(movements))
	for _, movement := range movements {
		views = append(views, newMovementView(movement))
	}

	httpx.Paginated(w, "Stock movements retrieved successfully.", views, httpx.NewMeta(page, limit, total))
}

// Adjust applies a manual correction. A reason is mandatory, so every
// quantity change can be explained later.
func (h *Handler) Adjust(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperror.Unauthorized)
		return
	}

	actorID, err := bson.ObjectIDFromHex(principal.UserID)
	if err != nil {
		httpx.Error(w, r, apperror.Unauthorized)
		return
	}

	var body adjustRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	productID, err := bson.ObjectIDFromHex(strings.TrimSpace(body.ProductID))
	if err != nil {
		httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"product_id": "is not a valid identifier"}))
		return
	}

	level, err := h.service.Adjust(r.Context(), application.AdjustInput{
		ProductID: productID,
		Quantity:  body.Quantity,
		Reason:    body.Reason,
	}, &application.Actor{ID: actorID, Role: principal.Role})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Stock adjusted successfully.", newLevelView(*level))
}

// newLevelView projects a stock level for the API.
func newLevelView(level application.StockLevel) levelView {
	return levelView{
		ProductID:    level.ProductID.Hex(),
		AvailableQty: level.Available,
		ReservedQty:  level.Reserved,
		Sellable:     level.Sellable,
		StockBadge:   level.Badge,
		UpdatedAt:    level.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// newMovementView projects a stock movement for the API.
func newMovementView(movement domain.Movement) movementView {
	view := movementView{
		ID:        movement.ID.Hex(),
		ProductID: movement.ProductID.Hex(),
		Type:      movement.Type,
		Quantity:  movement.Quantity,
		Reason:    movement.Reason,
		Reference: movement.Reference,
		CreatedAt: movement.CreatedAt.UTC().Format(time.RFC3339),
	}
	if movement.ActorID != nil {
		view.ActorID = movement.ActorID.Hex()
	}
	return view
}
