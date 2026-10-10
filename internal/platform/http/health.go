package httpx

import (
	"context"
	"net/http"
	"time"
)

// probeTimeout bounds each dependency check so a hung dependency cannot hold
// the readiness probe open.
const probeTimeout = 3 * time.Second

// Probe is a named dependency check used by the readiness endpoint.
type Probe struct {
	Name  string
	Check func(ctx context.Context) error
	// Optional marks a dependency the API can serve without. A failing
	// optional probe reports the service as degraded rather than not ready:
	// Redis being down delays emails and the reservation sweep, but orders
	// still commit and their events wait in the outbox, so answering 503 —
	// and with it taking the storefront out of a load balancer — would turn a
	// degraded queue into an outage.
	Optional bool
}

// HealthHandler serves the liveness and readiness endpoints.
type HealthHandler struct {
	version string
	probes  []Probe
}

// NewHealthHandler builds the handler with the dependency probes to report on.
func NewHealthHandler(version string, probes ...Probe) *HealthHandler {
	return &HealthHandler{version: version, probes: probes}
}

// Live reports that the process is running. It must not touch dependencies:
// an orchestrator restarts the container when this fails.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	OK(w, "Service is live.", map[string]any{
		"status":  "ok",
		"service": h.version,
	})
}

// Ready reports whether required dependencies are reachable. It returns 503
// when any probe fails, and never exposes connection strings or driver errors.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]string, len(h.probes))
	ready := true
	degraded := false

	for _, probe := range h.probes {
		ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
		err := probe.Check(ctx)
		cancel()

		if err != nil {
			checks[probe.Name] = "unavailable"
			if probe.Optional {
				degraded = true
			} else {
				ready = false
			}
			continue
		}
		checks[probe.Name] = "ok"
	}

	status := "ok"
	switch {
	case !ready:
		status = "not_ready"
	case degraded:
		status = "degraded"
	}

	payload := map[string]any{
		"status":     status,
		"service":    h.version,
		"checks":     checks,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	}

	if !ready {
		JSON(w, http.StatusServiceUnavailable, "Service is not ready.", payload)
		return
	}

	// Degraded still answers 200: the probe says what is wrong, and alerting
	// reads the body rather than only the status code.
	OK(w, "Service is ready.", payload)
}
