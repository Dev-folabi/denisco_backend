package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authapp "github.com/Dev-folabi/denisco_backend/internal/modules/auth/application"
	authdomain "github.com/Dev-folabi/denisco_backend/internal/modules/auth/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// The authentication tests run against MongoDB rather than in-memory
// repositories, because the properties that matter — one account per address,
// single-use refresh tokens, single-use reset tokens — are enforced by
// conditional updates and unique indexes rather than by Go code.

func TestAuthFlowAgainstMongo(t *testing.T) {
	e := require(t)
	c := ctx(t)
	service := e.container.AuthService

	registered, err := service.Register(c, authapp.RegisterInput{
		FirstName: "Ada",
		LastName:  "Obi",
		Email:     "Ada@Denisco.com",
		Phone:     "+2348012345678",
		Password:  "farm-secret-1",
	})
	if err != nil {
		t.Fatalf("Register() returned error: %v", err)
	}
	if registered.User.Email != "ada@denisco.com" {
		t.Errorf("stored email = %q, want it normalised to %q", registered.User.Email, "ada@denisco.com")
	}
	if registered.User.Role != usersdomain.RoleCustomer {
		t.Errorf("role = %q, want %q", registered.User.Role, usersdomain.RoleCustomer)
	}

	// Registration is announced through the outbox in the same request, so
	// the welcome email survives a crash before the worker runs.
	if count := e.outboxCount(t, "user.registered"); count != 1 {
		t.Errorf("user.registered events = %d, want 1", count)
	}

	if _, err := service.Register(c, authapp.RegisterInput{
		FirstName: "Someone",
		LastName:  "Else",
		Email:     "ada@denisco.com",
		Phone:     "+2348011111111",
		Password:  "farm-secret-2",
	}); !errors.Is(err, usersdomain.ErrEmailTaken) {
		t.Errorf("Register() with a taken email error = %v, want ErrEmailTaken", err)
	}

	logged, err := service.Login(c, authapp.LoginInput{
		Email:     "ada@denisco.com",
		Password:  "farm-secret-1",
		UserAgent: "integration-suite",
	})
	if err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}
	if logged.AccessToken == "" || logged.RefreshToken == "" {
		t.Fatal("Login() returned an incomplete session")
	}

	// The access token must verify under the same signer the middleware uses.
	claims, err := e.container.Signer.Verify(logged.AccessToken)
	if err != nil {
		t.Fatalf("Verify() on the issued access token returned error: %v", err)
	}
	if claims.Subject != logged.User.ID || claims.Role != usersdomain.RoleCustomer {
		t.Errorf("claims = (sub %q, role %q), want (%q, %q)", claims.Subject, claims.Role, logged.User.ID, usersdomain.RoleCustomer)
	}

	if _, err := service.Login(c, authapp.LoginInput{
		Email:    "ada@denisco.com",
		Password: "the-wrong-password",
	}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() with a wrong password error = %v, want ErrInvalidCredentials", err)
	}

	// A customer may not use the admin door.
	if _, err := service.AdminLogin(c, authapp.LoginInput{
		Email:    "ada@denisco.com",
		Password: "farm-secret-1",
	}); !errors.Is(err, authdomain.ErrAdminOnly) {
		t.Errorf("AdminLogin() as a customer error = %v, want ErrAdminOnly", err)
	}

	refreshed, err := service.Refresh(c, logged.RefreshToken, "integration-suite")
	if err != nil {
		t.Fatalf("Refresh() returned error: %v", err)
	}
	if refreshed.RefreshToken == logged.RefreshToken {
		t.Error("Refresh() returned the same token, want it rotated")
	}

	current, err := service.CurrentUser(c, refreshed.User.ID)
	if err != nil {
		t.Fatalf("CurrentUser() returned error: %v", err)
	}
	if current.Email != "ada@denisco.com" {
		t.Errorf("CurrentUser() email = %q, want %q", current.Email, "ada@denisco.com")
	}

	if err := service.Logout(c, refreshed.RefreshToken); err != nil {
		t.Fatalf("Logout() returned error: %v", err)
	}

	if _, err := service.Refresh(c, refreshed.RefreshToken, "integration-suite"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Errorf("Refresh() after logout error = %v, want ErrInvalidRefresh", err)
	}
}

func TestRefreshReuseRevokesTheWholeFamily(t *testing.T) {
	e := require(t)
	c := ctx(t)
	service := e.container.AuthService

	buyer := e.newCustomer(t, "reuse@denisco.com")

	first, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "farm-secret-1"})
	if err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}

	second, err := service.Refresh(c, first.RefreshToken, "integration-suite")
	if err != nil {
		t.Fatalf("Refresh() returned error: %v", err)
	}

	// Replaying the consumed token is the signal that it leaked.
	if _, err := service.Refresh(c, first.RefreshToken, "integration-suite"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Fatalf("Refresh() with a consumed token error = %v, want ErrInvalidRefresh", err)
	}

	// The successor is revoked too: the whole session family goes, because we
	// cannot tell the thief from the owner.
	if _, err := service.Refresh(c, second.RefreshToken, "integration-suite"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Errorf("Refresh() with the successor of a reused token error = %v, want ErrInvalidRefresh", err)
	}

	// Signing in again gives a fresh family that works.
	third, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "farm-secret-1"})
	if err != nil {
		t.Fatalf("Login() after a revoked family returned error: %v", err)
	}
	if _, err := service.Refresh(c, third.RefreshToken, "integration-suite"); err != nil {
		t.Errorf("Refresh() on the new family returned error: %v", err)
	}
}

func TestPasswordResetIsSingleUseAndEndsEverySession(t *testing.T) {
	e := require(t)
	c := ctx(t)
	service := e.container.AuthService

	buyer := e.newCustomer(t, "reset@denisco.com")

	session, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "farm-secret-1"})
	if err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}

	// The token is returned directly only because the test configuration
	// enables that development aid; in production it only reaches the email.
	token, err := service.ForgotPassword(c, buyer.Email)
	if err != nil {
		t.Fatalf("ForgotPassword() returned error: %v", err)
	}
	if token == "" {
		t.Fatal("ForgotPassword() returned no token")
	}

	if count := e.outboxCount(t, "password_reset.requested"); count != 1 {
		t.Errorf("password_reset.requested events = %d, want 1", count)
	}

	if err := service.ResetPassword(c, authapp.ResetPasswordInput{Token: token, Password: "new-farm-secret"}); err != nil {
		t.Fatalf("ResetPassword() returned error: %v", err)
	}

	// Redeeming the same link twice must fail even though the token is still
	// well-formed and unexpired.
	if err := service.ResetPassword(c, authapp.ResetPasswordInput{
		Token:    token,
		Password: "another-secret-1",
	}); !errors.Is(err, authdomain.ErrInvalidResetToken) {
		t.Errorf("ResetPassword() a second time error = %v, want ErrInvalidResetToken", err)
	}

	// Sessions opened before the reset are gone, which is the point of
	// resetting after a suspected compromise.
	if _, err := service.Refresh(c, session.RefreshToken, "integration-suite"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Errorf("Refresh() with a pre-reset token error = %v, want ErrInvalidRefresh", err)
	}

	if _, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "farm-secret-1"}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() with the old password error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "new-farm-secret"}); err != nil {
		t.Errorf("Login() with the new password returned error: %v", err)
	}
}

func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	e := require(t)
	c := ctx(t)
	service := e.container.AuthService

	buyer := e.newCustomer(t, "change@denisco.com")

	if err := service.ChangePassword(c, buyer.ID.Hex(), authapp.ChangePasswordInput{
		CurrentPassword: "not-the-password",
		NewPassword:     "brand-new-secret",
	}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("ChangePassword() with a wrong current password error = %v, want ErrInvalidCredentials", err)
	}

	if err := service.ChangePassword(c, buyer.ID.Hex(), authapp.ChangePasswordInput{
		CurrentPassword: "farm-secret-1",
		NewPassword:     "farm-secret-1",
	}); !errors.Is(err, authdomain.ErrSamePassword) {
		t.Errorf("ChangePassword() reusing the password error = %v, want ErrSamePassword", err)
	}

	if err := service.ChangePassword(c, buyer.ID.Hex(), authapp.ChangePasswordInput{
		CurrentPassword: "farm-secret-1",
		NewPassword:     "brand-new-secret",
	}); err != nil {
		t.Fatalf("ChangePassword() returned error: %v", err)
	}

	if _, err := service.Login(c, authapp.LoginInput{Email: buyer.Email, Password: "brand-new-secret"}); err != nil {
		t.Errorf("Login() with the changed password returned error: %v", err)
	}
}

func TestLoginOverHTTPSetsTheRefreshCookie(t *testing.T) {
	e := require(t)
	buyer := e.newCustomer(t, "http@denisco.com")

	body := strings.NewReader(`{"email":"` + buyer.Email + `","password":"farm-secret-1"}`)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")

	recorder := httptest.NewRecorder()
	e.api.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /auth/login returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken string `json:"access_token"`
			User        struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("could not decode the login response: %v", err)
	}
	if !response.Success || response.Data.AccessToken == "" {
		t.Errorf("response = %+v, want a successful login carrying an access token", response)
	}
	if response.Data.User.Email != buyer.Email {
		t.Errorf("response user = %q, want %q", response.Data.User.Email, buyer.Email)
	}

	// The refresh token belongs in an HttpOnly cookie scoped to the auth
	// routes, never in the response body.
	var refresh *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == e.container.Config.Auth.RefreshCookieName {
			refresh = cookie
		}
	}
	if refresh == nil {
		t.Fatalf("no %q cookie was set", e.container.Config.Auth.RefreshCookieName)
	}
	if !refresh.HttpOnly {
		t.Error("the refresh cookie is not HttpOnly")
	}
	if refresh.Path != e.container.Config.Auth.RefreshCookiePath {
		t.Errorf("refresh cookie path = %q, want %q", refresh.Path, e.container.Config.Auth.RefreshCookiePath)
	}
	if strings.Contains(recorder.Body.String(), refresh.Value) {
		t.Error("the refresh token appears in the response body, want it only in the cookie")
	}
}

// outboxCount counts the pending events of one type, so a test can assert
// that a workflow announced itself.
func (e *environment) outboxCount(t *testing.T, eventType string) int64 {
	t.Helper()

	count, err := e.db.Collection("outbox_events").CountDocuments(ctx(t), bson.M{"type": eventType})
	if err != nil {
		t.Fatalf("could not count %s events: %v", eventType, err)
	}

	return count
}
