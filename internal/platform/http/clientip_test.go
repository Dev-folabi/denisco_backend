package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The address ClientIP returns decides which bucket the rate limiter counts a
// request in and whose address appears in the audit log, so a caller must not
// be able to choose it.

func TestClientIPIgnoresHeadersByDefault(t *testing.T) {
	t.Cleanup(func() { TrustClientIPHeader("") })
	TrustClientIPHeader("")

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:54321"
	request.Header.Set("X-Forwarded-For", "1.1.1.1")
	request.Header.Set("X-Real-IP", "2.2.2.2")
	request.Header.Set("CF-Connecting-IP", "3.3.3.3")

	if got := ClientIP(request); got != "203.0.113.9" {
		t.Errorf("ClientIP() = %q, want the peer address 203.0.113.9: no header is trusted by default", got)
	}
}

func TestClientIPUsesTheConfiguredHeader(t *testing.T) {
	t.Cleanup(func() { TrustClientIPHeader("") })
	TrustClientIPHeader("CF-Connecting-IP")

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.18.0.5:40000" // Caddy, on the internal network
	request.Header.Set("CF-Connecting-IP", "198.51.100.7")
	// A forged chain must not win over the header the proxy writes.
	request.Header.Set("X-Forwarded-For", "1.1.1.1, 198.51.100.7")

	if got := ClientIP(request); got != "198.51.100.7" {
		t.Errorf("ClientIP() = %q, want 198.51.100.7 from the trusted header", got)
	}
}

func TestClientIPFallsBackWhenTheTrustedHeaderIsMissingOrAChain(t *testing.T) {
	t.Cleanup(func() { TrustClientIPHeader("") })
	TrustClientIPHeader("X-Real-IP")

	missing := httptest.NewRequest(http.MethodGet, "/", nil)
	missing.RemoteAddr = "172.18.0.5:40000"

	if got := ClientIP(missing); got != "172.18.0.5" {
		t.Errorf("ClientIP() = %q, want the peer address when the trusted header is absent", got)
	}

	// Which entry of a chain is the client depends on how many hops appended
	// to it, so a chain is not trusted even in the configured header.
	chained := httptest.NewRequest(http.MethodGet, "/", nil)
	chained.RemoteAddr = "172.18.0.5:40000"
	chained.Header.Set("X-Real-IP", "1.1.1.1, 2.2.2.2")

	if got := ClientIP(chained); got != "172.18.0.5" {
		t.Errorf("ClientIP() = %q, want the peer address when the trusted header carries a chain", got)
	}
}

func TestReadinessTreatsOptionalDependenciesAsDegraded(t *testing.T) {
	handler := NewHealthHandler("denisco-api",
		Probe{Name: "mongodb", Check: func(context.Context) error { return nil }},
		Probe{Name: "redis", Optional: true, Check: func(context.Context) error {
			return errors.New("connection refused")
		}},
	)

	recorder := httptest.NewRecorder()
	handler.Ready(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	// The queue being down delays emails; it does not stop the shop selling,
	// so this must not read as "do not send me traffic".
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d when only an optional dependency is down", recorder.Code, http.StatusOK)
	}

	status, checks := readinessBody(t, recorder)
	if status != "degraded" {
		t.Errorf("status = %q, want %q", status, "degraded")
	}
	if checks["redis"] != "unavailable" || checks["mongodb"] != "ok" {
		t.Errorf("checks = %v, want redis unavailable and mongodb ok", checks)
	}
}

func TestReadinessFailsWhenARequiredDependencyIsDown(t *testing.T) {
	handler := NewHealthHandler("denisco-api",
		Probe{Name: "mongodb", Check: func(context.Context) error {
			return errors.New("no reachable servers")
		}},
		Probe{Name: "redis", Optional: true, Check: func(context.Context) error { return nil }},
	)

	recorder := httptest.NewRecorder()
	handler.Ready(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d when the database is unreachable", recorder.Code, http.StatusServiceUnavailable)
	}

	status, _ := readinessBody(t, recorder)
	if status != "not_ready" {
		t.Errorf("status = %q, want %q", status, "not_ready")
	}
}

func TestLivenessDoesNotTouchDependencies(t *testing.T) {
	called := false
	handler := NewHealthHandler("denisco-api",
		Probe{Name: "mongodb", Check: func(context.Context) error {
			called = true
			return errors.New("unreachable")
		}},
	)

	recorder := httptest.NewRecorder()
	handler.Live(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: liveness reports only that the process runs", recorder.Code, http.StatusOK)
	}
	if called {
		t.Error("liveness ran a dependency probe; a failing dependency would restart the container")
	}
}

// readinessBody pulls the status and per-dependency checks out of a readiness
// response.
func readinessBody(t *testing.T, recorder *httptest.ResponseRecorder) (string, map[string]string) {
	t.Helper()

	var response struct {
		Data struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		} `json:"data"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("could not decode the readiness response: %v", err)
	}

	return response.Data.Status, response.Data.Checks
}

func TestSearchParamIsTrimmedAndBounded(t *testing.T) {
	// Search becomes a regular expression run over a collection, so the term's
	// length is work the server does per document.
	long := strings.Repeat("a", MaxSearchLength+50)

	cases := map[string]string{
		"  broiler  ": "broiler",
		"":            "",
		long:          strings.Repeat("a", MaxSearchLength),
	}

	for raw, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/products?search="+url.QueryEscape(raw), nil)
		if got := SearchParam(request); got != want {
			t.Errorf("SearchParam(%q) = %q (len %d), want %q", raw, got, len(got), want)
		}
	}
}
