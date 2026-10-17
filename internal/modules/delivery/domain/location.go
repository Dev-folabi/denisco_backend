// Package domain holds the home-delivery area entity, its repository
// contracts and the settings singleton that switches the offering on or off.
//
// The prototype priced home delivery at a flat ₦2,500 within Abuja. The farm
// now delivers to more than one area and each carries its own rate, so the
// fee lives on the location rather than in a constant.
package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Location errors.
var (
	ErrLocationNotFound = apperror.New(http.StatusNotFound, "DELIVERY_LOCATION_NOT_FOUND", "The delivery location could not be found.")
	ErrNameTaken        = apperror.New(http.StatusConflict, "DELIVERY_LOCATION_NAME_TAKEN", "A delivery location with that name already exists.")
)

// Location is a named delivery area with its own flat rate, in kobo.
type Location struct {
	ID   bson.ObjectID `bson:"_id,omitempty"`
	Name string        `bson:"name"`
	// NameKey is the lowercased name, so the uniqueness rule reads the same
	// however the administrator capitalises it.
	NameKey   string    `bson:"name_key"`
	Fee       int64     `bson:"fee"`
	Active    bool      `bson:"active"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Repository is the persistence contract for delivery locations.
type Repository interface {
	// List returns locations by display name; activeOnly hides retired ones.
	List(ctx context.Context, activeOnly bool) ([]Location, error)

	// FindByID returns a location or ErrLocationNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*Location, error)

	// Create inserts a location.
	Create(ctx context.Context, location *Location) error

	// Update applies a set of changed fields.
	Update(ctx context.Context, id bson.ObjectID, fields bson.M) error

	// Delete removes a location.
	Delete(ctx context.Context, id bson.ObjectID) error
}

// Settings is the home-delivery master switch, stored as one document.
//
// It defaults to enabled: a shop that has just gained the feature should not
// be silently closed. The public listing still shows nothing until at least
// one location is active, so an empty catalogue is what stops the offering
// at the checkout, not this flag.
type Settings struct {
	Enabled   bool      `bson:"enabled"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// DefaultEnabled is the switch's value before an administrator has set it.
const DefaultEnabled = true

// SettingsRepository is the persistence contract for the master switch.
type SettingsRepository interface {
	// Get returns the stored settings, or the defaults when none has been
	// written yet.
	Get(ctx context.Context) (Settings, error)

	// SetEnabled stores the switch.
	SetEnabled(ctx context.Context, enabled bool) error
}
