// Package application coordinates the home-delivery offering: the areas the
// farm delivers to, each with its own rate, and the master switch.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	"github.com/Dev-folabi/denisco_backend/internal/modules/delivery/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit actions recorded for administrative changes.
const (
	actionLocationCreated = "delivery_location.created"
	actionLocationUpdated = "delivery_location.updated"
	actionLocationDeleted = "delivery_location.deleted"
	actionSettingsChanged = "delivery_settings.changed"
)

// Service implements the home-delivery workflows.
type Service struct {
	locations domain.Repository
	settings  domain.SettingsRepository
	auditor   audit.Recorder
}

// NewService wires the delivery module to its repositories.
func NewService(locations domain.Repository, settings domain.SettingsRepository, auditor audit.Recorder) *Service {
	return &Service{locations: locations, settings: settings, auditor: auditor}
}

// LocationView is the public projection of a delivery area.
type LocationView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Fee       int64  `json:"fee"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Offering is what the checkout reads: whether home delivery is on at all,
// and the areas a customer may choose between.
type Offering struct {
	Enabled   bool           `json:"enabled"`
	Locations []LocationView `json:"locations"`
}

// Options returns the customer-facing offering. Home delivery reads as
// enabled only when the switch is on AND at least one area is active — an
// empty list is the offering closed, whatever the switch says.
func (s *Service) Options(ctx context.Context) (*Offering, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return nil, apperror.From(err)
	}

	locations, err := s.locations.List(ctx, true)
	if err != nil {
		return nil, apperror.From(err)
	}

	views := make([]LocationView, 0, len(locations))
	for _, location := range locations {
		views = append(views, newLocationView(location))
	}

	// While the switch is off there is nothing on offer, so the areas are
	// withheld rather than sent for the checkout to ignore: the public view
	// is a promise of what a customer can be sold, not a preview.
	enabled := settings.Enabled && len(views) > 0
	if !enabled {
		views = nil
	}

	return &Offering{
		Enabled:   enabled,
		Locations: views,
	}, nil
}

// Admin returns the master switch and every location, retired ones included,
// for the settings page.
func (s *Service) Admin(ctx context.Context) (*Offering, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return nil, apperror.From(err)
	}

	locations, err := s.locations.List(ctx, false)
	if err != nil {
		return nil, apperror.From(err)
	}

	views := make([]LocationView, 0, len(locations))
	for _, location := range locations {
		views = append(views, newLocationView(location))
	}

	return &Offering{Enabled: settings.Enabled, Locations: views}, nil
}

// CheckoutLocation returns a sellable area by id. The bool is false when the
// area cannot be delivered to — unknown, retired, or the whole offering
// switched off — which is all one answer to a checkout: no.
func (s *Service) CheckoutLocation(ctx context.Context, id bson.ObjectID) (domain.Location, bool, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return domain.Location{}, false, apperror.From(err)
	}
	if !settings.Enabled {
		return domain.Location{}, false, nil
	}

	location, err := s.locations.FindByID(ctx, id)
	if err != nil {
		if err == domain.ErrLocationNotFound {
			return domain.Location{}, false, nil
		}
		return domain.Location{}, false, apperror.From(err)
	}
	if !location.Active {
		return domain.Location{}, false, nil
	}

	return *location, true, nil
}

// LocationInput is a create-or-update request; nil fields are left alone.
type LocationInput struct {
	Name   *string
	Fee    *int64
	Active *bool
}

// SetEnabledInput switches the offering on or off.
type SetEnabledInput struct {
	Enabled *bool
}

// CreateLocation adds a delivery area.
func (s *Service) CreateLocation(ctx context.Context, input LocationInput, actor Actor) (*LocationView, error) {
	if input.Name == nil || input.Fee == nil {
		return nil, apperror.Validation.WithDetails(map[string]any{
			"name": "name and fee are required",
		})
	}

	name, err := validateName(*input.Name)
	if err != nil {
		return nil, err
	}
	fee, err := validateFee(*input.Fee)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNameFree(ctx, name, bson.ObjectID{}); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	location := &domain.Location{
		ID:        bson.NewObjectID(),
		Name:      name,
		NameKey:   strings.ToLower(name),
		Fee:       fee,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if input.Active != nil {
		location.Active = *input.Active
	}

	if err := s.locations.Create(ctx, location); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionLocationCreated, location.ID.Hex(), map[string]any{
		"name": location.Name,
		"fee":  location.Fee,
	})

	view := newLocationView(*location)
	return &view, nil
}

// UpdateLocation changes a delivery area's name, rate or standing.
func (s *Service) UpdateLocation(ctx context.Context, id string, input LocationInput, actor Actor) (*LocationView, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrLocationNotFound
	}

	fields := bson.M{}

	if input.Name != nil {
		name, err := validateName(*input.Name)
		if err != nil {
			return nil, err
		}
		if err := s.ensureNameFree(ctx, name, objectID); err != nil {
			return nil, err
		}
		fields["name"] = name
		fields["name_key"] = strings.ToLower(name)
	}
	if input.Fee != nil {
		fee, err := validateFee(*input.Fee)
		if err != nil {
			return nil, err
		}
		fields["fee"] = fee
	}
	if input.Active != nil {
		fields["active"] = *input.Active
	}

	if err := s.locations.Update(ctx, objectID, fields); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionLocationUpdated, id, nil)

	updated, err := s.locations.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	view := newLocationView(*updated)
	return &view, nil
}

// DeleteLocation removes a delivery area. An order that was delivered there
// keeps its own snapshot of the name and the fee, so history does not break.
func (s *Service) DeleteLocation(ctx context.Context, id string, actor Actor) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return domain.ErrLocationNotFound
	}

	if err := s.locations.Delete(ctx, objectID); err != nil {
		return apperror.From(err)
	}

	s.record(ctx, actor, actionLocationDeleted, id, nil)
	return nil
}

// SetEnabled switches home delivery on or off for every area at once.
func (s *Service) SetEnabled(ctx context.Context, input SetEnabledInput, actor Actor) (*Offering, error) {
	if input.Enabled == nil {
		return nil, apperror.Validation.WithDetails(map[string]any{"enabled": "is required"})
	}

	if err := s.settings.SetEnabled(ctx, *input.Enabled); err != nil {
		return nil, apperror.From(err)
	}

	s.record(ctx, actor, actionSettingsChanged, "", map[string]any{"enabled": *input.Enabled})

	return s.Admin(ctx)
}

// ensureNameFree rejects a duplicate name, comparing case-insensitively
// through the stored key so "Wuse" and "wuse" are the same place.
func (s *Service) ensureNameFree(ctx context.Context, name string, ownID bson.ObjectID) error {
	existing, err := s.locations.List(ctx, false)
	if err != nil {
		return apperror.From(err)
	}

	key := strings.ToLower(name)
	for _, location := range existing {
		if location.NameKey == key && location.ID != ownID {
			return domain.ErrNameTaken
		}
	}
	return nil
}

// Actor identifies the administrator behind a change.
type Actor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}

// record writes an audit entry for an administrative change.
func (s *Service) record(ctx context.Context, actor Actor, action, resourceID string, metadata map[string]any) {
	if s.auditor == nil || actor.ID.IsZero() {
		return
	}

	id := actor.ID
	s.auditor.Record(ctx, audit.Entry{
		ActorID:    &id,
		ActorEmail: actor.Email,
		ActorRole:  actor.Role,
		Action:     action,
		Resource:   "delivery_location",
		ResourceID: resourceID,
		Metadata:   metadata,
	})
}

// newLocationView projects a location into its API representation.
func newLocationView(location domain.Location) LocationView {
	return LocationView{
		ID:        location.ID.Hex(),
		Name:      location.Name,
		Fee:       location.Fee,
		Active:    location.Active,
		CreatedAt: location.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: location.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// validateName trims and bounds an area's name.
func validateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "is required"})
	}
	if len(trimmed) > 120 {
		return "", apperror.Validation.WithDetails(map[string]any{"name": "must be 120 characters or fewer"})
	}
	return trimmed, nil
}

// validateFee bounds a rate, which arrives in kobo.
func validateFee(fee int64) (int64, error) {
	if fee < 0 {
		return 0, apperror.Validation.WithDetails(map[string]any{"fee": "cannot be negative"})
	}
	if fee > 10_000_000_000 {
		return 0, apperror.Validation.WithDetails(map[string]any{
			"fee": "is unrealistically large; amounts are in kobo",
		})
	}
	return fee, nil
}
