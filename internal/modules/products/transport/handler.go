// Package transport exposes the catalogue over HTTP.
package transport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	"github.com/Dev-folabi/denisco_backend/internal/modules/products/application"
	"github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/platform/storage"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// relatedProductLimit matches the product detail page in the prototype.
const relatedProductLimit = 4

// Handler serves the product endpoints.
type Handler struct {
	service *application.Service
	media   *storage.ImageKit
	auditor audit.Recorder
}

// NewHandler builds the handler for the products module.
func NewHandler(service *application.Service, media *storage.ImageKit, auditor audit.Recorder) *Handler {
	return &Handler{service: service, media: media, auditor: auditor}
}

// Request bodies.
type mediaPayload struct {
	URL    string `json:"url"`
	FileID string `json:"file_id,omitempty"`
}

type createProductRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Category    string         `json:"category"`
	Price       int64          `json:"price"`
	Unit        string         `json:"unit"`
	Images      []mediaPayload `json:"images"`
	Status      string         `json:"status"`
	Featured    bool           `json:"featured"`
	Stock       int            `json:"stock"`
}

type updateProductRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Category    *string         `json:"category"`
	Price       *int64          `json:"price"`
	Unit        *string         `json:"unit"`
	Images      *[]mediaPayload `json:"images"`
	Status      *string         `json:"status"`
	Featured    *bool           `json:"featured"`
	Stock       *int            `json:"stock"`
}

type uploadSignatureRequest struct {
	Folder string `json:"folder"`
}

// List returns active products for the storefront.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	products, total, err := h.service.List(r.Context(), application.ListInput{
		CategorySlug: r.URL.Query().Get("category"),
		// The storefront only ever sees listed products, whatever the client asks.
		Status:   domain.StatusActive,
		Featured: featuredParam(r),
		Search:   httpx.SearchParam(r),
		Sort:     r.URL.Query().Get("sort"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Products retrieved successfully.", products, httpx.NewMeta(page, limit, total))
}

// Get returns one active product by id.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	product, err := h.service.GetByID(r.Context(), chi.URLParam(r, "id"), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Product retrieved successfully.", product)
}

// GetBySlug returns one active product by slug, which is how the shop links to
// a product detail page.
func (h *Handler) GetBySlug(w http.ResponseWriter, r *http.Request) {
	product, err := h.service.GetBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Product retrieved successfully.", product)
}

// Related returns other products in the same category.
func (h *Handler) Related(w http.ResponseWriter, r *http.Request) {
	product, err := h.service.GetBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	related, err := h.service.Related(r.Context(), product.ID, product.Category, relatedProductLimit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Related products retrieved successfully.", related)
}

// ListAdmin returns every product, archived ones included.
func (h *Handler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.PageParams(r)

	products, total, err := h.service.ListAdmin(r.Context(), application.ListInput{
		CategorySlug: r.URL.Query().Get("category"),
		Status:       r.URL.Query().Get("status"),
		Featured:     featuredParam(r),
		Search:       httpx.SearchParam(r),
		Sort:         r.URL.Query().Get("sort"),
		Page:         page,
		Limit:        limit,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Paginated(w, "Products retrieved successfully.", products, httpx.NewMeta(page, limit, total))
}

// GetAdmin returns one product, archived ones included.
func (h *Handler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	product, err := h.service.GetByID(r.Context(), chi.URLParam(r, "id"), false)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Product retrieved successfully.", product)
}

// Create adds a product.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body createProductRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	images, err := h.validateImages(body.Images)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	product, err := h.service.Create(r.Context(), application.CreateInput{
		Name:         body.Name,
		Description:  body.Description,
		CategorySlug: body.Category,
		Price:        body.Price,
		Unit:         body.Unit,
		Images:       images,
		Status:       body.Status,
		Featured:     body.Featured,
		Stock:        body.Stock,
	}, actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.Created(w, "Product created successfully.", product)
}

// Update applies a partial change to a product.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var body updateProductRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	input := application.UpdateInput{
		Name:         body.Name,
		Description:  body.Description,
		CategorySlug: body.Category,
		Price:        body.Price,
		Unit:         body.Unit,
		Status:       body.Status,
		Featured:     body.Featured,
		Stock:        body.Stock,
	}

	if body.Images != nil {
		images, err := h.validateImages(*body.Images)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		input.Images = &images
	}

	product, err := h.service.Update(r.Context(), chi.URLParam(r, "id"), input, actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Product updated successfully.", product)
}

// Remove deletes a product, or archives it when orders refer to it.
//
// The response says which happened, both in the message the dashboard shows
// and in the body, so an admin is never told something was deleted when it was
// in fact archived.
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.service.Remove(r.Context(), chi.URLParam(r, "id"), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if result.Deleted {
		httpx.OK(w, "Product deleted successfully.", result)
		return
	}

	httpx.OK(w, "Product archived successfully because it has orders.", result)
}

// UploadSignature issues a short-lived ImageKit credential so the admin
// browser can upload straight to the media host.
func (h *Handler) UploadSignature(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// The body is optional: callers may name a folder or accept the default.
	body := uploadSignatureRequest{}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &body); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}

	folder := strings.TrimSpace(body.Folder)
	if folder == "" {
		folder = "/denisco/products"
	}

	signature, err := h.media.NewUploadSignature(folder)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if h.auditor != nil {
		id := actor.ID
		h.auditor.Record(r.Context(), audit.Entry{
			ActorID:    &id,
			ActorEmail: actor.Email,
			ActorRole:  actor.Role,
			Action:     audit.ActionUploadSignatureIssued,
			Resource:   "media",
			Metadata:   map[string]any{"folder": folder},
		})
	}

	httpx.OK(w, "Upload signature issued successfully.", signature)
}

// validateImages checks every image URL belongs to the configured media
// library, so the admin API cannot be used to point a product at any host.
func (h *Handler) validateImages(payload []mediaPayload) ([]application.Media, error) {
	images := make([]application.Media, 0, len(payload))

	for _, item := range payload {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		if err := h.media.ValidateURL(item.URL); err != nil {
			return nil, err
		}
		images = append(images, application.Media{URL: strings.TrimSpace(item.URL), FileID: item.FileID})
	}

	return images, nil
}

// actor builds the audit identity of the signed-in administrator.
func (h *Handler) actor(r *http.Request) (application.StockActor, error) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return application.StockActor{}, apperror.Unauthorized
	}

	id, err := bson.ObjectIDFromHex(principal.UserID)
	if err != nil {
		return application.StockActor{}, apperror.Unauthorized
	}

	return application.StockActor{ID: id, Role: principal.Role}, nil
}

// featuredParam reads the optional `featured` filter.
func featuredParam(r *http.Request) *bool {
	raw := strings.TrimSpace(r.URL.Query().Get("featured"))
	if raw == "" {
		return nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &value
}
