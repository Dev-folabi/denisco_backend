package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// preflightMaxAge is how long a browser may cache a preflight result.
const preflightMaxAge = 10 * time.Minute

// allowedMethods and allowedHeaders are deliberately narrow: only what the two
// known frontends send.
var (
	allowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions}
	allowedHeaders = []string{"Authorization", "Content-Type", "X-Request-ID", "X-CSRF-Token"}
)

// CORS allows credentialed requests from the exact configured origins only.
// A wildcard is impossible here by design: cookies require an exact origin.
func CORS(origins []string) func(http.Handler) http.Handler {
	methods := strings.Join(allowedMethods, ", ")
	headers := strings.Join(allowedHeaders, ", ")
	maxAge := strconv.Itoa(int(preflightMaxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && slices.Contains(origins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
				// Responses differ per origin, so caches must not share them.
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")

				if origin == "" || !slices.Contains(origins, origin) {
					httpx.Error(w, r, apperror.Forbidden.WithDetails(map[string]any{"origin": "not allowed"}))
					return
				}

				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFOriginGuard protects cookie-authenticated endpoints (refresh, logout).
//
// Those endpoints authenticate with a cookie rather than a bearer token, so a
// cross-site page could trigger them. Browsers send Origin on every
// cross-origin request and on all same-origin POSTs, so requiring an approved
// Origin (falling back to Referer) blocks forged submissions. The refresh
// cookie additionally carries SameSite, which stops cross-site sends outright.
func CSRFOriginGuard(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe methods cannot change state.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser clients omit Origin; fall back to Referer so a
				// forged form submission still cannot pass.
				if referer := r.Header.Get("Referer"); referer != "" {
					origin = originOf(referer)
				}
			}

			if origin == "" || !slices.Contains(origins, origin) {
				httpx.Error(w, r, apperror.Forbidden.WithDetails(map[string]any{
					"origin": "the request origin is not allowed for this endpoint",
				}))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// originOf reduces a URL to its scheme://host[:port] form.
func originOf(rawURL string) string {
	scheme, rest, found := strings.Cut(rawURL, "://")
	if !found {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
