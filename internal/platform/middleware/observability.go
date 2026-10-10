package middleware

import (
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"github.com/go-chi/chi/v5"
)

// requestIDHeader is both read (to honour an upstream ID) and written.
const requestIDHeader = "X-Request-ID"

// RequestID attaches a correlation ID to the context, the response header and
// the request-scoped logger.
func RequestID(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if id == "" {
				generated, err := security.NewID()
				if err != nil {
					// An ID is diagnostic, never a reason to fail a request.
					generated = "unknown"
				}
				id = generated
			}

			w.Header().Set(requestIDHeader, id)

			ctx := withRequestID(r.Context(), id)
			ctx = logger.WithContext(ctx, log.With(slog.String("request_id", id)))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// statusRecorder captures the status and size for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// RequestLogger emits one structured entry per request. Query strings and
// bodies are never logged because they may carry credentials.
func RequestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Int("bytes", recorder.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("ip", httpx.ClientIP(r)),
			}

			if route := chi.RouteContext(r.Context()); route != nil && route.RoutePattern() != "" {
				attrs = append(attrs, slog.String("route", route.RoutePattern()))
			}
			if principal, ok := PrincipalFromContext(r.Context()); ok {
				attrs = append(attrs, slog.String("user_id", principal.UserID), slog.String("role", principal.Role))
			}

			log := logger.FromContext(r.Context())
			switch {
			case recorder.status >= http.StatusInternalServerError:
				log.Error("request completed", attrs...)
			case recorder.status >= http.StatusBadRequest:
				log.Warn("request completed", attrs...)
			default:
				log.Info("request completed", attrs...)
			}
		})
	}
}

// Recoverer converts a panic into a 500 response instead of dropping the
// connection, and logs the cause with the stack trace.
func Recoverer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				// A client disconnect surfaces as this sentinel; re-panic so
				// net/http handles it as it normally would.
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}

				logger.FromContext(r.Context()).Error("panic recovered",
					slog.Any("panic", recovered),
					slog.String("stack", stackTrace()),
				)

				httpx.Error(w, r, apperror.Internal)
			}()

			next.ServeHTTP(w, r)
		})
	}
}
