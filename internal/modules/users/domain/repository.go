package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Repository is the persistence contract for user accounts. It is declared
// next to the domain that uses it; the MongoDB implementation lives in the
// module's infrastructure package.
type Repository interface {
	// Create persists a new user. It returns ErrEmailTaken when the email is
	// already registered.
	Create(ctx context.Context, user *User) error

	// FindByID returns the user or ErrUserNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*User, error)

	// FindByEmail returns the user for a normalized email, or ErrUserNotFound.
	FindByEmail(ctx context.Context, email string) (*User, error)

	// UpdatePasswordHash replaces the stored hash.
	UpdatePasswordHash(ctx context.Context, id bson.ObjectID, hash string) error

	// TouchLastLogin records a successful authentication.
	TouchLastLogin(ctx context.Context, id bson.ObjectID, at time.Time) error
}

// AdminRepository is the administrative view of user accounts, kept separate
// from the authentication-facing contract above because it serves a different
// consumer with different access rules.
type AdminRepository interface {
	// List returns a page of customers, newest first, optionally filtered by
	// a name or email search.
	List(ctx context.Context, filter AdminFilter, page, limit int) ([]User, int64, error)

	// Count returns the number of accounts with a role.
	Count(ctx context.Context, role string) (int64, error)

	// FindByID returns one account or ErrUserNotFound.
	FindByID(ctx context.Context, id bson.ObjectID) (*User, error)
}

// AdminFilter narrows an administrative customer listing.
type AdminFilter struct {
	// Role limits results to one role; empty means customers only.
	Role string
	// Search matches the name and email.
	Search string
}
