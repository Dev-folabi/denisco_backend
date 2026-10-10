package infrastructure

import (
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// apperrorNotFound is the error returned when a slot id matches nothing.
func apperrorNotFound() error {
	return apperror.New(http.StatusNotFound, "SLOT_NOT_FOUND", "The time slot could not be found.")
}
