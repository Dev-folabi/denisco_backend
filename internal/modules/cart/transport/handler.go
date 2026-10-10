// Package transport exposes the cart over HTTP. Every route is
// customer-authenticated: a cart always belongs to the signed-in user, which
// is why no cart or user identifier appears in any path.
package transport

import (
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/modules/cart/application"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Handler serves the cart endpoints.
type Handler struct {
	service *application.Service
}

// NewHandler builds the handler for the cart module.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Mount registers the cart routes. The caller has already applied
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/cart", func(cart chi.Router) {
		cart.Get("/", h.Get)
		cart.Delete("/", h.Clear)
		cart.Post("/items", h.AddItem)
		cart.Patch("/items/{productID}", h.SetQuantity)
		cart.Delete("/items/{productID}", h.RemoveItem)
	})
}

type addItemRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type setQuantityRequest struct {
	Quantity int `json:"quantity"`
}

// Get returns the cart priced at current catalogue prices.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := customerID(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	cart, err := h.service.Get(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Cart retrieved successfully.", cart)
}

// AddItem adds a product to the cart.
func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	userID, err := customerID(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body addItemRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	productID, err := bson.ObjectIDFromHex(body.ProductID)
	if err != nil {
		httpx.Error(w, r, apperror.Validation.WithDetails(map[string]any{"product_id": "is not a valid identifier"}))
		return
	}

	quantity := body.Quantity
	if quantity == 0 {
		quantity = 1
	}

	cart, err := h.service.AddItem(r.Context(), userID, productID, quantity)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Item added to your cart.", cart)
}

// SetQuantity replaces a line's quantity.
func (h *Handler) SetQuantity(w http.ResponseWriter, r *http.Request) {
	userID, productID, err := customerAndProduct(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body setQuantityRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	cart, err := h.service.SetQuantity(r.Context(), userID, productID, body.Quantity)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Cart updated successfully.", cart)
}

// RemoveItem drops a line from the cart.
func (h *Handler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	userID, productID, err := customerAndProduct(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	cart, err := h.service.RemoveItem(r.Context(), userID, productID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Item removed from your cart.", cart)
}

// Clear empties the cart.
func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	userID, err := customerID(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	cart, err := h.service.Clear(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Cart cleared successfully.", cart)
}

// customerID returns the signed-in customer's identifier.
func customerID(r *http.Request) (bson.ObjectID, error) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return bson.ObjectID{}, apperror.Unauthorized
	}

	id, err := bson.ObjectIDFromHex(principal.UserID)
	if err != nil {
		return bson.ObjectID{}, apperror.Unauthorized
	}

	return id, nil
}

// customerAndProduct reads the caller and the addressed cart line.
func customerAndProduct(r *http.Request) (bson.ObjectID, bson.ObjectID, error) {
	userID, err := customerID(r)
	if err != nil {
		return bson.ObjectID{}, bson.ObjectID{}, err
	}

	productID, err := bson.ObjectIDFromHex(chi.URLParam(r, "productID"))
	if err != nil {
		return bson.ObjectID{}, bson.ObjectID{}, apperror.NotFound.WithDetails(map[string]any{"item": "is not a valid cart item"})
	}

	return userID, productID, nil
}
