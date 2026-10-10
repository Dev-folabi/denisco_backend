package transport

import "github.com/go-chi/chi/v5"

// MountPublic registers the storefront catalogue routes.
func (h *Handler) MountPublic(r chi.Router) {
	r.Route("/products", func(products chi.Router) {
		products.Get("/", h.List)
		// The slug routes are declared before "/{id}" so a slug is never
		// mistaken for an identifier.
		products.Get("/slug/{slug}", h.GetBySlug)
		products.Get("/slug/{slug}/related", h.Related)
		products.Get("/{id}", h.Get)
	})
}

// MountAdmin registers the catalogue management routes. The caller has already
// applied authentication and the admin role check.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Route("/products", func(products chi.Router) {
		products.Get("/", h.ListAdmin)
		products.Post("/", h.Create)
		products.Post("/upload-signature", h.UploadSignature)
		products.Get("/{id}", h.GetAdmin)
		products.Patch("/{id}", h.Update)
		products.Delete("/{id}", h.Remove)
	})
}
