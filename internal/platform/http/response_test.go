package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

func TestOKEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	OK(rec, "Profile retrieved successfully.", map[string]string{"id": "abc"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Success bool              `json:"success"`
		Message string            `json:"message"`
		Data    map[string]string `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if !body.Success || body.Message == "" || body.Data["id"] != "abc" {
		t.Errorf("unexpected envelope: %+v", body)
	}
}

func TestPaginatedMeta(t *testing.T) {
	meta := NewMeta(1, 20, 45)
	if meta.TotalPages != 3 {
		t.Errorf("TotalPages = %d, want 3", meta.TotalPages)
	}

	if exact := NewMeta(1, 20, 40); exact.TotalPages != 2 {
		t.Errorf("TotalPages = %d, want 2", exact.TotalPages)
	}

	if empty := NewMeta(1, 20, 0); empty.TotalPages != 0 {
		t.Errorf("TotalPages = %d, want 0", empty.TotalPages)
	}
}

func TestErrorHidesInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)

	Error(rec, req, apperror.Internal.Wrap(errSecretDatabaseFailure))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	raw := rec.Body.String()
	if strings.Contains(raw, "connection string") {
		t.Errorf("response leaked the underlying cause: %s", raw)
	}

	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if body.Success {
		t.Error("error envelope reported success")
	}
	if body.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("error code = %q, want INTERNAL_ERROR", body.Error.Code)
	}
}

func TestErrorPreservesDomainErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)

	Error(rec, req, apperror.Validation.WithDetails(map[string]any{"email": "is required"}))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	var body struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if body.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error code = %q, want VALIDATION_ERROR", body.Error.Code)
	}
	if body.Error.Details["email"] != "is required" {
		t.Errorf("details = %v, want the field error", body.Error.Details)
	}
}

// errSecretDatabaseFailure stands in for an infrastructure error whose text
// must never reach a client.
var errSecretDatabaseFailure = &testError{"mongo: auth failed for connection string"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
