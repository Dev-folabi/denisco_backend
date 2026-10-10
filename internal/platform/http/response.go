// Package httpx holds the HTTP primitives shared by every module: the response
// envelope, request decoding and the single place where application errors
// become HTTP responses.
//
// The package lives in internal/platform/http but is named httpx so files can
// import net/http alongside it without an alias.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// envelope is the success shape required by DENISCO_Architecture.md §6.1.
type envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Meta    *Meta  `json:"meta,omitempty"`
}

// Meta carries pagination information for list responses.
type Meta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// NewMeta builds pagination metadata, deriving the page count from the total.
func NewMeta(page, limit int, total int64) *Meta {
	totalPages := 0
	if limit > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}
	return &Meta{Page: page, Limit: limit, Total: total, TotalPages: totalPages}
}

// errorEnvelope is the failure shape. The code and details follow the
// architecture document; message is repeated inside error for clients that
// read error.message.
type errorEnvelope struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Error   errorBody `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// JSON writes a success envelope with the given status.
func JSON(w http.ResponseWriter, status int, message string, data any) {
	write(w, status, envelope{Success: true, Message: message, Data: data})
}

// OK writes a 200 success envelope.
func OK(w http.ResponseWriter, message string, data any) {
	JSON(w, http.StatusOK, message, data)
}

// Created writes a 201 success envelope.
func Created(w http.ResponseWriter, message string, data any) {
	JSON(w, http.StatusCreated, message, data)
}

// Paginated writes a 200 success envelope with pagination metadata.
func Paginated(w http.ResponseWriter, message string, data any, meta *Meta) {
	write(w, http.StatusOK, envelope{Success: true, Message: message, Data: data, Meta: meta})
}

// NoContent writes a 204 with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error maps any error to its HTTP response. Non-application errors become an
// opaque 500 so internal details never reach the client; the cause is logged
// with the request-scoped logger instead.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	appErr := apperror.From(err)

	log := logger.FromContext(r.Context())
	attrs := []any{
		slog.String("error_code", appErr.Code),
		slog.Int("status", appErr.Status),
	}
	if appErr.Status >= http.StatusInternalServerError {
		log.Error("request failed", append(attrs, slog.String("error", appErr.Error()))...)
	} else {
		log.Warn("request rejected", append(attrs, slog.String("reason", appErr.Message))...)
	}

	write(w, appErr.Status, errorEnvelope{
		Success: false,
		Message: appErr.Message,
		Error: errorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		},
	})
}

// write serialises the body. A marshalling failure after the status is sent
// cannot be recovered, so it is logged rather than retried.
func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("failed to encode response body", slog.String("error", err.Error()))
	}
}

// APIPrefix is the version prefix every business route sits behind.
const APIPrefix = "/api/v1"
