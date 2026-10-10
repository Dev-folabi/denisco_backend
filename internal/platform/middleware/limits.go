package middleware

import (
	"net/http"
	"time"

	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/httprate"
)

// BodyLimit caps the request body so a large upload cannot exhaust memory.
// Decode surfaces the overflow as a 413.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit throttles by client IP over the given window. Sensitive routes
// (login, registration, password reset) get their own stricter limiter.
func RateLimit(requests int, window time.Duration) func(http.Handler) http.Handler {
	if requests <= 0 {
		return passthrough
	}

	return httprate.Limit(
		requests,
		window,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
			return httpx.ClientIP(r), nil
		}),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, apperror.TooManyReqs)
		}),
	)
}

// passthrough is used when a limiter is disabled by configuration.
func passthrough(next http.Handler) http.Handler { return next }
