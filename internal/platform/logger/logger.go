// Package logger configures the structured logger used across the API, worker
// and outbox processes.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// contextKey is private so only this package can seed the context logger.
type contextKey struct{}

// New returns a JSON logger writing to stdout at the configured level.
func New(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: ParseLevel(level)})
	return slog.New(handler)
}

// ParseLevel maps a configured level name to a slog level, defaulting to info.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithContext stores a logger (already carrying request-scoped fields such as
// request_id) so downstream layers log with the same correlation attributes.
func WithContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext returns the request-scoped logger, or the default logger when
// the context carries none.
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}
