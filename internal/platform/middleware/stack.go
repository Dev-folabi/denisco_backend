package middleware

import "runtime/debug"

// stackTrace returns the current goroutine's stack for panic diagnostics.
func stackTrace() string {
	return string(debug.Stack())
}
