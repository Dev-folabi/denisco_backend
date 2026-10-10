package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
)

// newSigner builds a signer for middleware tests.
func newSigner(t *testing.T) *security.TokenSigner {
	t.Helper()

	signer, err := security.NewTokenSigner(security.SignerOptions{
		Secret:   "a-test-secret-that-is-long-enough-to-pass",
		Issuer:   "denisco_backend",
		Audience: "denisco_api",
		TTL:      15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}
	return signer
}

// okHandler records that the request reached the end of the chain.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthenticateRejectsMissingToken(t *testing.T) {
	var reached bool
	handler := Authenticate(newSigner(t))(okHandler(&reached))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if reached {
		t.Error("the handler ran without authentication")
	}
}

func TestAuthenticateRejectsMalformedHeader(t *testing.T) {
	var reached bool
	handler := Authenticate(newSigner(t))(okHandler(&reached))

	for _, header := range []string{"token-without-scheme", "Basic abc", "Bearer", "Bearer    "} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Header.Set("Authorization", header)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want %d", header, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestAuthenticateAcceptsValidTokenAndSetsPrincipal(t *testing.T) {
	signer := newSigner(t)

	token, _, err := signer.Sign("65f0c0ffee0000000000beef", "admin")
	if err != nil {
		t.Fatalf("Sign() returned error: %v", err)
	}

	var principal Principal
	handler := Authenticate(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := PrincipalFromContext(r.Context())
		if !ok {
			t.Error("the principal was not stored in the request context")
		}
		principal = got
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if principal.UserID != "65f0c0ffee0000000000beef" {
		t.Errorf("user id = %q, want the token subject", principal.UserID)
	}
	if principal.Role != "admin" {
		t.Errorf("role = %q, want \"admin\"", principal.Role)
	}
}

func TestRequireRole(t *testing.T) {
	signer := newSigner(t)

	cases := []struct {
		name       string
		tokenRole  string
		allowed    []string
		wantStatus int
	}{
		{"admin on admin route", "admin", []string{"admin", "super_admin"}, http.StatusOK},
		{"customer on admin route", "customer", []string{"admin", "super_admin"}, http.StatusForbidden},
		{"super admin on admin route", "super_admin", []string{"admin", "super_admin"}, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, _, err := signer.Sign("65f0c0ffee0000000000beef", tc.tokenRole)
			if err != nil {
				t.Fatalf("Sign() returned error: %v", err)
			}

			var reached bool
			handler := Authenticate(signer)(RequireRole(tc.allowed...)(okHandler(&reached)))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != (tc.wantStatus == http.StatusOK) {
				t.Errorf("handler reached = %v, want %v", reached, tc.wantStatus == http.StatusOK)
			}
		})
	}
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	origins := []string{"http://localhost:3000", "http://localhost:3001"}

	var reached bool
	handler := CORS(origins)(okHandler(&reached))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the request origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want \"true\"", got)
	}

	// An unapproved origin receives no CORS headers, so the browser blocks the
	// response. The request itself still runs.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://evil.example")

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for an unapproved origin", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := CORS([]string{"http://localhost:3000"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the preflight reached the handler")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("the preflight response is missing Access-Control-Allow-Methods")
	}

	// An unapproved origin must not receive a usable preflight.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d for an unapproved preflight", rec.Code, http.StatusForbidden)
	}
}

func TestCSRFOriginGuard(t *testing.T) {
	origins := []string{"http://localhost:3000"}

	cases := []struct {
		name       string
		method     string
		origin     string
		referer    string
		wantStatus int
	}{
		{"approved origin", http.MethodPost, "http://localhost:3000", "", http.StatusOK},
		{"foreign origin", http.MethodPost, "https://evil.example", "", http.StatusForbidden},
		{"no origin or referer", http.MethodPost, "", "", http.StatusForbidden},
		{"approved referer fallback", http.MethodPost, "", "http://localhost:3000/checkout", http.StatusOK},
		{"foreign referer fallback", http.MethodPost, "", "https://evil.example/page", http.StatusForbidden},
		{"safe method passes", http.MethodGet, "", "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			handler := CSRFOriginGuard(origins)(okHandler(&reached))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/api/v1/auth/refresh", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestRecovererReturnsInternalError(t *testing.T) {
	handler := Recoverer()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/products", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !contains(body, "INTERNAL_ERROR") {
		t.Errorf("body = %s, want the opaque internal error code", body)
	}
	if body := rec.Body.String(); contains(body, "boom") {
		t.Errorf("body leaked the panic value: %s", body)
	}
}

func TestRequestIDIsEchoedAndPreserved(t *testing.T) {
	var seen string
	handler := RequestID(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// A generated ID is returned to the caller.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if seen == "" {
		t.Error("no request id was stored in the context")
	}
	if rec.Header().Get("X-Request-ID") != seen {
		t.Errorf("X-Request-ID header = %q, want %q", rec.Header().Get("X-Request-ID"), seen)
	}

	// An upstream ID (set by Caddy) is honoured so traces join up.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "upstream-id")

	handler.ServeHTTP(rec, req)

	if seen != "upstream-id" {
		t.Errorf("request id = %q, want the upstream value", seen)
	}
}

func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	handler := BodyLimit(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		if _, err := r.Body.Read(buf); err != nil && err.Error() != "EOF" {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", stringReader("a much longer body than permitted"))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
