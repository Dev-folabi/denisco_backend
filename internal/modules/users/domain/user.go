// Package domain holds the user entity and the rules that govern it. It must
// not depend on HTTP, MongoDB or any other infrastructure.
package domain

import (
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Role values. Authorization is always enforced server-side against these.
const (
	RoleCustomer   = "customer"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// Account status values.
const (
	StatusActive    = "active"
	StatusInactive  = "inactive"
	StatusSuspended = "suspended"
)

// Domain errors for the user entity.
var (
	ErrEmailTaken    = apperror.New(http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "An account with this email address already exists.")
	ErrUserNotFound  = apperror.New(http.StatusNotFound, "USER_NOT_FOUND", "The user could not be found.")
	ErrUserInactive  = apperror.New(http.StatusForbidden, "ACCOUNT_INACTIVE", "This account is not active. Please contact support.")
	ErrUserSuspended = apperror.New(http.StatusForbidden, "ACCOUNT_SUSPENDED", "This account has been suspended. Please contact support.")
)

// User is a customer or administrative account.
type User struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	FirstName     string        `bson:"first_name"`
	LastName      string        `bson:"last_name"`
	Email         string        `bson:"email"` // normalized, unique
	Phone         string        `bson:"phone"`
	PasswordHash  string        `bson:"password_hash"`
	Role          string        `bson:"role"`
	Status        string        `bson:"status"`
	EmailVerified bool          `bson:"email_verified"`
	LastLoginAt   *time.Time    `bson:"last_login_at,omitempty"`
	CreatedAt     time.Time     `bson:"created_at"`
	UpdatedAt     time.Time     `bson:"updated_at"`
}

// FullName is the display name used in emails and the dashboards.
func (u *User) FullName() string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// IsAdmin reports whether the account may access the admin dashboard.
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

// EnsureActive returns the reason an account may not authenticate, if any.
func (u *User) EnsureActive() error {
	switch u.Status {
	case StatusActive:
		return nil
	case StatusSuspended:
		return ErrUserSuspended
	default:
		return ErrUserInactive
	}
}

// NormalizeEmail lower-cases and trims an address so lookups and the unique
// index treat "A@B.com" and "a@b.com" as the same account.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail checks the address is parseable and normalizes it.
func ValidateEmail(email string) (string, error) {
	normalized := NormalizeEmail(email)
	if normalized == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"email": "is required"})
	}
	if len(normalized) > 254 {
		return "", apperror.Validation.WithDetails(map[string]any{"email": "must be 254 characters or fewer"})
	}

	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized {
		return "", apperror.Validation.WithDetails(map[string]any{"email": "is not a valid email address"})
	}

	// RFC 5322 permits a dotless domain such as "a@b"; a deliverable customer
	// address always has a public suffix, so require one.
	_, domain, _ := strings.Cut(normalized, "@")
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", apperror.Validation.WithDetails(map[string]any{"email": "is not a valid email address"})
	}

	return normalized, nil
}

// NormalizePhone strips spacing and punctuation that users commonly type, so
// stored numbers are comparable.
func NormalizePhone(phone string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(phone) {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidatePhone accepts an optional Nigerian or international number.
func ValidatePhone(phone string) (string, error) {
	normalized := NormalizePhone(phone)
	if normalized == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"phone": "is required"})
	}

	digits := strings.TrimPrefix(normalized, "+")
	if len(digits) < 10 || len(digits) > 15 {
		return "", apperror.Validation.WithDetails(map[string]any{"phone": "must contain between 10 and 15 digits"})
	}

	return normalized, nil
}

// ValidateName trims and bounds a personal name.
func ValidateName(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", apperror.Validation.WithDetails(map[string]any{field: "is required"})
	}
	if len(trimmed) > 60 {
		return "", apperror.Validation.WithDetails(map[string]any{field: "must be 60 characters or fewer"})
	}
	return trimmed, nil
}
