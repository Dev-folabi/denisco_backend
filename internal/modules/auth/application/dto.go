// Package application coordinates the authentication workflows: registration,
// login, token rotation, password reset and password change. It owns the
// business rules and never touches HTTP types.
package application

import (
	"strconv"
	"strings"
	"time"

	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// RegisterInput is a customer self-registration request.
type RegisterInput struct {
	FirstName string
	LastName  string
	Email     string
	Phone     string
	Password  string
}

// LoginInput is a credential submission.
type LoginInput struct {
	Email    string
	Password string
	// UserAgent is stored with the session for administrative visibility.
	UserAgent string
}

// ResetPasswordInput redeems a password-reset token.
type ResetPasswordInput struct {
	Token    string
	Password string
}

// ChangePasswordInput updates the signed-in user's own password.
type ChangePasswordInput struct {
	CurrentPassword string
	NewPassword     string
}

// UserView is the public projection of a user account. The password hash and
// internal fields are never exposed.
type UserView struct {
	ID            string `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	EmailVerified bool   `json:"email_verified"`
	CreatedAt     string `json:"created_at"`
}

// newUserView projects a domain user into its API representation.
func newUserView(user *usersdomain.User) UserView {
	return UserView{
		ID:            user.ID.Hex(),
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		FullName:      user.FullName(),
		Email:         user.Email,
		Phone:         user.Phone,
		Role:          user.Role,
		Status:        user.Status,
		EmailVerified: user.EmailVerified,
		CreatedAt:     user.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// Session is the result of a successful authentication. The access token goes
// in the response body; the refresh token is handed to the transport layer to
// set as an HttpOnly cookie and is never serialised into JSON.
type Session struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	AccessExpiresIn  int
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             UserView
}

// validateNewPassword enforces the password policy. The upper bound exists
// because bcrypt ignores input beyond 72 bytes, and silently truncating would
// weaken the password without telling anyone.
func validateNewPassword(password string, min, max int) error {
	if strings.TrimSpace(password) == "" {
		return apperror.Validation.WithDetails(map[string]any{"password": "is required"})
	}
	if len(password) < min {
		return apperror.Validation.WithDetails(map[string]any{
			"password": "must be at least " + strconv.Itoa(min) + " characters",
		})
	}
	if len(password) > max {
		return apperror.Validation.WithDetails(map[string]any{
			"password": "must be at most " + strconv.Itoa(max) + " characters",
		})
	}
	return nil
}
