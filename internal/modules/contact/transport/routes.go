package transport

import (
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/go-chi/chi/v5"
)

// Mount registers the contact routes.
//
// There is one, and it is public: the contact page is the way a visitor with
// no account reaches the farm. It carries its own limiter because it is the
// one anonymous endpoint whose cost is a real message in someone's inbox, and
// a flood of them is worse than a flood of catalogue reads.
func (h *Handler) Mount(r chi.Router, rateLimit config.RateLimitConfig) {
	requests := rateLimit.ContactPerHour
	if !rateLimit.Enabled {
		requests = 0
	}

	r.With(middleware.RateLimit(requests, time.Hour)).Post("/contact", h.Submit)
}
