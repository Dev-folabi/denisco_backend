// Package application exposes the category read model.
package application

import (
	"context"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// Service reads categories for the shop's filter chips and the admin forms.
type Service struct {
	repo domain.Repository
}

// NewService wires the service to its repository.
func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo}
}

// View is the public projection of a category.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Position    int    `json:"position"`
	Active      bool   `json:"active"`
	CreatedAt   string `json:"created_at"`
}

// List returns categories in display order.
func (s *Service) List(ctx context.Context, activeOnly bool) ([]View, error) {
	categories, err := s.repo.List(ctx, activeOnly)
	if err != nil {
		return nil, apperror.From(err)
	}

	views := make([]View, 0, len(categories))
	for _, category := range categories {
		views = append(views, View{
			ID:          category.ID.Hex(),
			Name:        category.Name,
			Slug:        category.Slug,
			Description: category.Description,
			Position:    category.Position,
			Active:      category.Active,
			CreatedAt:   category.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return views, nil
}
