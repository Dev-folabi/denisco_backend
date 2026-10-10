package transport

import (
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/go-chi/chi/v5"
)

// RouteDeps carries the cross-cutting middleware the auth routes need, so the
// module does not reach into the platform wiring itself.
type RouteDeps struct {
	// Authenticate validates the bearer access token.
	Authenticate func(http.Handler) http.Handler
	// RateLimit holds the per-endpoint request budgets.
	RateLimit config.RateLimitConfig
	// Origins are the exact frontend origins allowed on cookie endpoints.
	Origins []string
}

// Mount registers the auth routes on the given router.
//
// Credential endpoints carry the strictest rate limits. The two
// cookie-authenticated endpoints (refresh, logout) additionally pass the CSRF
// origin guard, because they do not use a bearer token.
func (h *Handler) Mount(r chi.Router, deps RouteDeps) {
	// Each endpoint gets its own limiter instance: sharing one would mean a
	// customer who requested two reset emails could no longer submit the new
	// password. A zero budget disables the limiter, which is how
	// RATE_LIMIT_ENABLED=false is honoured here.
	limit := func(requests int, window time.Duration) func(http.Handler) http.Handler {
		if !deps.RateLimit.Enabled {
			requests = 0
		}
		return middleware.RateLimit(requests, window)
	}

	csrf := middleware.CSRFOriginGuard(deps.Origins)

	r.Route("/auth", func(auth chi.Router) {
		auth.With(limit(deps.RateLimit.SignupPerHour, time.Hour)).Post("/register", h.Register)
		auth.With(limit(deps.RateLimit.LoginPerMin, time.Minute)).Post("/login", h.Login)
		auth.With(limit(deps.RateLimit.LoginPerMin, time.Minute)).Post("/admin/login", h.AdminLogin)
		auth.With(limit(deps.RateLimit.ResetPerHour, time.Hour)).Post("/forgot-password", h.ForgotPassword)
		auth.With(limit(deps.RateLimit.ResetPerHour, time.Hour)).Post("/reset-password", h.ResetPassword)

		auth.With(csrf).Post("/refresh", h.Refresh)
		auth.With(csrf).Post("/logout", h.Logout)

		auth.Group(func(protected chi.Router) {
			protected.Use(deps.Authenticate)
			protected.Get("/me", h.Me)
			protected.Post("/change-password", h.ChangePassword)
		})
	})
}
