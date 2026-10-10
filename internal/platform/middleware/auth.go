package middleware

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"

	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// TokenVerifier is the slice of the signer the middleware needs, so the
// middleware package does not depend on how tokens are issued.
type TokenVerifier interface {
	Verify(token string) (*security.Claims, error)
}

// Authenticate validates the bearer token and stores the principal. A missing
// or invalid token is rejected here; route-level authorization comes next.
func Authenticate(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				httpx.Error(w, r, apperror.Unauthorized.WithDetails(map[string]any{
					"authorization": "a bearer access token is required",
				}))
				return
			}

			claims, err := verifier.Verify(token)
			if err != nil {
				// Log the rejection without the token itself.
				logger.FromContext(r.Context()).Warn("access token rejected",
					slog.String("reason", "verification failed"),
				)
				httpx.Error(w, r, apperror.Unauthorized.WithDetails(map[string]any{
					"authorization": "the access token is invalid or has expired",
				}).Wrap(err))
				return
			}

			principal := Principal{UserID: claims.Subject, Role: claims.Role, TokenID: claims.ID}

			ctx := withPrincipal(r.Context(), principal)
			ctx = logger.WithContext(ctx, logger.FromContext(ctx).With(
				slog.String("user_id", principal.UserID),
			))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole authorizes the authenticated caller against a role allow-list.
// It must run after Authenticate.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				httpx.Error(w, r, apperror.Unauthorized)
				return
			}

			if !slices.Contains(roles, principal.Role) {
				logger.FromContext(r.Context()).Warn("authorization denied",
					slog.String("user_id", principal.UserID),
					slog.String("role", principal.Role),
					slog.String("path", r.URL.Path),
				)
				httpx.Error(w, r, apperror.Forbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts the credential from the Authorization header.
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}

	return token, true
}

// OptionalAuthenticate attaches the principal when a valid bearer token is
// present and proceeds regardless.
//
// The consultation page is public — a visitor can book without an account —
// but a signed-in customer's booking should still be attached to them so it
// appears in their history. An invalid token is treated as no token rather
// than an error, so a stale session never blocks a public page.
func OptionalAuthenticate(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := verifier.Verify(token)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			principal := Principal{UserID: claims.Subject, Role: claims.Role, TokenID: claims.ID}

			ctx := withPrincipal(r.Context(), principal)
			ctx = logger.WithContext(ctx, logger.FromContext(ctx).With(
				slog.String("user_id", principal.UserID),
			))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
