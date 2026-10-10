package bootstrap

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
)

// RouterOptions configures the base router.
type RouterOptions struct {
	Config config.Config
	Logger *slog.Logger
	Health *httpx.HealthHandler
	// Mount registers the versioned module routes.
	Mount func(r chi.Router)
}

// NewRouter assembles the middleware chain and mounts the health endpoints
// plus the versioned API.
//
// Middleware order matters: the correlation ID comes first so everything after
// it can log with the ID; recovery sits above logging so a panic is still
// logged as a completed request; CORS runs before rate limiting so a throttled
// preflight still carries the right headers.
func NewRouter(opts RouterOptions) chi.Router {
	cfg := opts.Config

	r := chi.NewRouter()

	r.Use(middleware.RequestID(opts.Logger))
	r.Use(middleware.Recoverer())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORS(cfg.CORS.AllowedOrigins()))
	r.Use(middleware.BodyLimit(cfg.HTTP.MaxBodyBytes))

	if cfg.RateLimit.Enabled {
		r.Use(middleware.RateLimit(cfg.RateLimit.GeneralPerMin, time.Minute))
	}

	// Health endpoints stay outside the version prefix so probes do not change
	// when the API is versioned. /healthz is the liveness alias used by Docker.
	r.Get("/healthz", opts.Health.Live)
	r.Get("/health/live", opts.Health.Live)
	r.Get("/health/ready", opts.Health.Ready)

	r.Route(httpx.APIPrefix, func(api chi.Router) {
		if opts.Mount != nil {
			opts.Mount(api)
		}
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apperror.NotFound.WithDetails(map[string]any{"path": r.URL.Path}))
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "That method is not supported on this endpoint."))
	})

	return r
}
