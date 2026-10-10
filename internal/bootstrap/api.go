package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/go-chi/chi/v5"
)

// NewAPIHandler builds the fully wired HTTP handler for the API process.
func NewAPIHandler(c *Container) http.Handler {
	probes := []httpx.Probe{
		{
			Name: "mongodb",
			Check: func(ctx context.Context) error {
				return database.Ping(ctx, c.Mongo)
			},
		},
		{
			Name: "redis",
			Check: func(ctx context.Context) error {
				if c.Redis == nil {
					return errors.New("redis is not configured")
				}
				return c.Redis.Ping(ctx)
			},
			// The queue is not on the path of a page view or a checkout.
			Optional: true,
		},
	}

	// Rate limiting and the audit log attribute requests to this address, so
	// which header may set it is a deployment decision, not a default.
	httpx.TrustClientIPHeader(c.Config.HTTP.ClientIPHeader)

	authenticate := middleware.Authenticate(c.Signer)

	return NewRouter(RouterOptions{
		Config: c.Config,
		Logger: c.Logger,
		Health: httpx.NewHealthHandler(c.Config.App.Name, probes...),
		Mount: func(api chi.Router) {
			c.AuthHandler.Mount(api, authtransportDeps(c, authenticate))

			// Public catalogue.
			c.CategoryHandler.Mount(api)
			c.ProductHandler.MountPublic(api)

			// Provider callbacks: authenticated by signature, not by session,
			// so they sit outside the authenticated groups below.
			c.PaymentHandler.MountWebhooks(api)

			optionalAuth := middleware.OptionalAuthenticate(c.Signer)

			// The consultation page is public and booking is open to guests,
			// so optional authentication attaches an account when there is
			// one without requiring it.
			c.ConsultationHandler.Mount(api, optionalAuth, authenticate)

			// Payments sit beside it for the same reason: a guest paying a
			// consultation fee has no session, so the subtree applies
			// authentication per route rather than to all of it.
			c.PaymentHandler.MountCustomer(api, optionalAuth, authenticate)

			// Customer area.
			api.Group(func(customer chi.Router) {
				customer.Use(authenticate)

				c.CartHandler.Mount(customer)
				c.OrderHandler.MountCustomer(customer)
			})

			// Administrative surface: authenticated, then role-checked once
			// for the whole group rather than per route.
			api.Route("/admin", func(admin chi.Router) {
				admin.Use(authenticate)
				admin.Use(middleware.RequireRole(usersdomain.RoleAdmin, usersdomain.RoleSuperAdmin))

				c.ProductHandler.MountAdmin(admin)
				c.InventoryHandler.MountAdmin(admin)
				c.OrderHandler.MountAdmin(admin)
				c.PaymentHandler.MountAdmin(admin)
				c.ConsultationHandler.MountAdmin(admin)
				c.AdminHandler.Mount(admin)
			})
		},
	})
}

// RunAPI starts the HTTP server and blocks until the process is signalled,
// then drains in-flight requests before releasing connections.
func RunAPI(c *Container) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.Config.App.Port),
		Handler:           NewAPIHandler(c),
		ReadTimeout:       c.Config.HTTP.ReadTimeout,
		ReadHeaderTimeout: c.Config.HTTP.ReadTimeout,
		WriteTimeout:      c.Config.HTTP.WriteTimeout,
		IdleTimeout:       c.Config.HTTP.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		c.Logger.Info("api listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("bootstrap: api server: %w", err)
	case <-ctx.Done():
		c.Logger.Info("shutdown signal received; draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), c.Config.HTTP.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("bootstrap: graceful shutdown: %w", err)
	}

	c.Logger.Info("api stopped cleanly", slog.Duration("drain_timeout", c.Config.HTTP.ShutdownTimeout))
	return nil
}

// wait is used by the worker and outbox processes, which have no listener of
// their own yet, to block until the process is signalled.
func wait(ctx context.Context, log *slog.Logger, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received")
			return
		case <-ticker.C:
		}
	}
}
