package middleware

import (
	"io"
	"log/slog"
	"strings"
)

// testLogger discards output so tests stay quiet.
func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// stringReader adapts a string to an io.Reader for request bodies.
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}

// contains reports whether haystack contains needle.
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
