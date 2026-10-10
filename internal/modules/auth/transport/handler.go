package transport

import (
	"net/http"

	"github.com/Dev-folabi/denisco_backend/internal/modules/auth/application"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	httpx "github.com/Dev-folabi/denisco_backend/internal/platform/http"
	"github.com/Dev-folabi/denisco_backend/internal/platform/middleware"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// Handler serves the authentication endpoints.
type Handler struct {
	service *application.Service
	cookies *cookieManager
}

// NewHandler builds the handler for the auth module.
func NewHandler(service *application.Service, authCfg config.AuthConfig) *Handler {
	return &Handler{service: service, cookies: newCookieManager(authCfg)}
}

// Request bodies. Field names match what the web and admin clients send.
type registerRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Password  string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// sessionResponse is the body returned by register, login and refresh. The
// refresh token is intentionally absent: it travels only in the cookie.
type sessionResponse struct {
	AccessToken string               `json:"access_token"`
	TokenType   string               `json:"token_type"`
	ExpiresIn   int                  `json:"expires_in"`
	User        application.UserView `json:"user"`
}

// Register creates a customer account.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.Register(r.Context(), application.RegisterInput{
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Email:     body.Email,
		Phone:     body.Phone,
		Password:  body.Password,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.cookies.set(w, session.RefreshToken, session.RefreshExpiresAt)
	httpx.Created(w, "Account created successfully.", newSessionResponse(session))
}

// Login authenticates a customer.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	h.handleLogin(w, r, false)
}

// AdminLogin authenticates an administrator for the dashboard.
func (h *Handler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	h.handleLogin(w, r, true)
}

// handleLogin is shared by the customer and admin login endpoints.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request, adminOnly bool) {
	var body loginRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	input := application.LoginInput{
		Email:     body.Email,
		Password:  body.Password,
		UserAgent: r.UserAgent(),
	}

	login := h.service.Login
	if adminOnly {
		login = h.service.AdminLogin
	}

	session, err := login(r.Context(), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.cookies.set(w, session.RefreshToken, session.RefreshExpiresAt)
	httpx.OK(w, "Signed in successfully.", newSessionResponse(session))
}

// Refresh rotates the refresh cookie and returns a new access token.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.Refresh(r.Context(), h.cookies.read(r), r.UserAgent())
	if err != nil {
		// The cookie is useless now; clearing it stops a refresh loop.
		h.cookies.clear(w)
		httpx.Error(w, r, err)
		return
	}

	h.cookies.set(w, session.RefreshToken, session.RefreshExpiresAt)
	httpx.OK(w, "Session refreshed successfully.", newSessionResponse(session))
}

// Logout revokes the session and clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Logout(r.Context(), h.cookies.read(r)); err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.cookies.clear(w)
	httpx.OK(w, "Signed out successfully.", nil)
}

// ForgotPassword starts the password-reset flow. The response is identical
// whether or not the address is registered.
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var body forgotPasswordRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	token, err := h.service.ForgotPassword(r.Context(), body.Email)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	const message = "If an account exists for that email address, a password reset link has been sent."

	// Only populated when AUTH_EXPOSE_RESET_TOKEN is enabled outside
	// production, so local development can complete the flow without email.
	if token != "" {
		httpx.OK(w, message, map[string]any{"reset_token": token})
		return
	}

	httpx.OK(w, message, nil)
}

// ResetPassword redeems a reset token.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body resetPasswordRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.ResetPassword(r.Context(), application.ResetPasswordInput{
		Token:    body.Token,
		Password: body.Password,
	}); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// Every session was revoked, so the stale cookie must go too.
	h.cookies.clear(w)
	httpx.OK(w, "Password reset successfully. Please sign in with your new password.", nil)
}

// ChangePassword updates the signed-in user's password.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperror.Unauthorized)
		return
	}

	var body changePasswordRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.service.ChangePassword(r.Context(), principal.UserID, application.ChangePasswordInput{
		CurrentPassword: body.CurrentPassword,
		NewPassword:     body.NewPassword,
	}); err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.cookies.clear(w)
	httpx.OK(w, "Password changed successfully. Please sign in again.", nil)
}

// Me returns the authenticated user's profile.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, apperror.Unauthorized)
		return
	}

	user, err := h.service.CurrentUser(r.Context(), principal.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.OK(w, "Profile retrieved successfully.", user)
}

// newSessionResponse projects a session into its response body.
func newSessionResponse(session *application.Session) sessionResponse {
	return sessionResponse{
		AccessToken: session.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   session.AccessExpiresIn,
		User:        session.User,
	}
}
