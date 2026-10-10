package bootstrap

import (
	"net/http"

	authtransport "github.com/Dev-folabi/denisco_backend/internal/modules/auth/transport"
)

// authtransportDeps assembles the cross-cutting middleware the auth module
// needs. Keeping the construction here means the module depends on a small
// struct rather than on the platform wiring.
func authtransportDeps(c *Container, authenticate func(http.Handler) http.Handler) authtransport.RouteDeps {
	return authtransport.RouteDeps{
		Authenticate: authenticate,
		RateLimit:    c.Config.RateLimit,
		Origins:      c.Config.CORS.AllowedOrigins(),
	}
}
