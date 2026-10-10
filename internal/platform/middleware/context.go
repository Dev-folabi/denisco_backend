// Package middleware holds the HTTP middleware chain: correlation IDs,
// logging, panic recovery, CORS, CSRF origin checks, rate limiting, body
// limits, authentication and authorization.
package middleware

import "context"

// Context keys are unexported types so no other package can collide with or
// overwrite request-scoped values.
type (
	requestIDKey struct{}
	principalKey struct{}
)

// Principal is the authenticated caller extracted from the access token.
type Principal struct {
	UserID string
	Role   string
	// TokenID is the JWT jti, logged to correlate a request with a session.
	TokenID string
}

// RequestIDFromContext returns the correlation ID for the current request.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// PrincipalFromContext returns the authenticated caller, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// withRequestID stores the correlation ID.
func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// withPrincipal stores the authenticated caller.
func withPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}
