// Package apperror defines the application error type used to translate
// domain and application failures into consistent HTTP responses.
//
// Handlers never build HTTP errors by hand: application services return an
// *apperror.Error, and the transport layer maps it with a single helper.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an application error carrying everything the HTTP layer needs:
// the status to return, a stable machine code for clients, a human-readable
// message and optional structured details.
//
// The wrapped err (if any) is for logging only and is never serialised.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
	err     error
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.err }

// Is matches application errors by their stable machine code.
//
// WithDetails and Wrap return clones, so a sentinel such as
// domain.ErrOrderNotFound is no longer pointer-identical by the time it
// reaches the caller. Comparing codes keeps errors.Is working against the
// sentinel the error came from, which is how callers branch on a condition.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return e.Code == other.Code
}

// WithDetails attaches structured details that are safe to return to clients.
func (e *Error) WithDetails(details map[string]any) *Error {
	clone := *e
	clone.Details = details
	return &clone
}

// Wrap attaches a cause for logging without changing the client-facing shape.
func (e *Error) Wrap(err error) *Error {
	clone := *e
	clone.err = err
	return &clone
}

// New builds an application error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// From returns err as an *Error when possible, otherwise an opaque internal
// error that keeps the cause for logs but hides it from the response.
func From(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal.Wrap(err)
}

// Shared errors used across modules. Modules define their own domain-specific
// errors next to the rules they belong to.
var (
	BadRequest   = New(http.StatusBadRequest, "BAD_REQUEST", "The request could not be understood.")
	Validation   = New(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "The submitted data is invalid.")
	Unauthorized = New(http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required.")
	Forbidden    = New(http.StatusForbidden, "FORBIDDEN", "You are not allowed to perform this action.")
	NotFound     = New(http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.")
	Conflict     = New(http.StatusConflict, "CONFLICT", "The resource is in a conflicting state.")
	TooManyReqs  = New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Please try again later.")
	PayloadLarge = New(http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The request body is too large.")
	Internal     = New(http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred.")
	Unavailable  = New(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is temporarily unavailable.")
)
