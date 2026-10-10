package application

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "github.com/Dev-folabi/denisco_backend/internal/modules/auth/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---------------------------------------------------------------------------
// In-memory fakes. They implement the repository contracts so the workflows
// can be exercised without MongoDB; the atomic operations mirror the
// conditional updates the real repositories perform.
// ---------------------------------------------------------------------------

type fakeUserRepo struct {
	byID    map[string]*usersdomain.User
	byEmail map[string]*usersdomain.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:    map[string]*usersdomain.User{},
		byEmail: map[string]*usersdomain.User{},
	}
}

func (f *fakeUserRepo) Create(_ context.Context, user *usersdomain.User) error {
	if _, exists := f.byEmail[user.Email]; exists {
		return usersdomain.ErrEmailTaken
	}
	if user.ID.IsZero() {
		user.ID = bson.NewObjectID()
	}
	clone := *user
	f.byID[user.ID.Hex()] = &clone
	f.byEmail[user.Email] = &clone
	return nil
}

func (f *fakeUserRepo) FindByID(_ context.Context, id bson.ObjectID) (*usersdomain.User, error) {
	user, ok := f.byID[id.Hex()]
	if !ok {
		return nil, usersdomain.ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}

func (f *fakeUserRepo) FindByEmail(_ context.Context, email string) (*usersdomain.User, error) {
	user, ok := f.byEmail[usersdomain.NormalizeEmail(email)]
	if !ok {
		return nil, usersdomain.ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}

func (f *fakeUserRepo) UpdatePasswordHash(_ context.Context, id bson.ObjectID, hash string) error {
	user, ok := f.byID[id.Hex()]
	if !ok {
		return usersdomain.ErrUserNotFound
	}
	user.PasswordHash = hash
	return nil
}

func (f *fakeUserRepo) TouchLastLogin(_ context.Context, id bson.ObjectID, at time.Time) error {
	if user, ok := f.byID[id.Hex()]; ok {
		user.LastLoginAt = &at
	}
	return nil
}

type fakeRefreshRepo struct {
	byHash map[string]*authdomain.RefreshToken
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{byHash: map[string]*authdomain.RefreshToken{}}
}

func (f *fakeRefreshRepo) Create(_ context.Context, token *authdomain.RefreshToken) error {
	clone := *token
	f.byHash[token.TokenHash] = &clone
	return nil
}

func (f *fakeRefreshRepo) FindByHash(_ context.Context, hash string) (*authdomain.RefreshToken, error) {
	token, ok := f.byHash[hash]
	if !ok {
		return nil, authdomain.ErrInvalidRefresh
	}
	clone := *token
	return &clone, nil
}

// Rotate mirrors the conditional update: it succeeds only while the token is
// still unconsumed.
func (f *fakeRefreshRepo) Rotate(_ context.Context, hash, successorHash string) (bool, error) {
	token, ok := f.byHash[hash]
	if !ok || token.Revoked {
		return false, nil
	}
	now := time.Now().UTC()
	token.Revoked = true
	token.RevokedAt = &now
	token.ReplacedBy = successorHash
	return true, nil
}

func (f *fakeRefreshRepo) RevokeFamily(_ context.Context, familyID string) error {
	for _, token := range f.byHash {
		if token.FamilyID == familyID {
			token.Revoked = true
		}
	}
	return nil
}

func (f *fakeRefreshRepo) RevokeAllForUser(_ context.Context, userID bson.ObjectID) error {
	for _, token := range f.byHash {
		if token.UserID == userID {
			token.Revoked = true
		}
	}
	return nil
}

type fakeResetRepo struct {
	byHash map[string]*authdomain.PasswordReset
}

func newFakeResetRepo() *fakeResetRepo {
	return &fakeResetRepo{byHash: map[string]*authdomain.PasswordReset{}}
}

func (f *fakeResetRepo) Create(_ context.Context, reset *authdomain.PasswordReset) error {
	clone := *reset
	f.byHash[reset.TokenHash] = &clone
	return nil
}

func (f *fakeResetRepo) FindByHash(_ context.Context, hash string) (*authdomain.PasswordReset, error) {
	reset, ok := f.byHash[hash]
	if !ok {
		return nil, authdomain.ErrInvalidResetToken
	}
	clone := *reset
	return &clone, nil
}

func (f *fakeResetRepo) Consume(_ context.Context, hash string) (bool, error) {
	reset, ok := f.byHash[hash]
	if !ok || reset.UsedAt != nil || !time.Now().UTC().Before(reset.ExpiresAt) {
		return false, nil
	}
	now := time.Now().UTC()
	reset.UsedAt = &now
	return true, nil
}

func (f *fakeResetRepo) CountSince(_ context.Context, userID bson.ObjectID, since time.Time) (int64, error) {
	var count int64
	for _, reset := range f.byHash {
		if reset.UserID == userID && !reset.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

type fakeOutbox struct {
	events []string
}

func (f *fakeOutbox) Append(_ context.Context, eventType, _ string, _ map[string]any) error {
	f.events = append(f.events, eventType)
	return nil
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type harness struct {
	service *Service
	users   *fakeUserRepo
	refresh *fakeRefreshRepo
	resets  *fakeResetRepo
	events  *fakeOutbox
}

// newHarness builds a service backed by in-memory repositories.
func newHarness(t *testing.T) *harness {
	t.Helper()

	users := newFakeUserRepo()
	refresh := newFakeRefreshRepo()
	resets := newFakeResetRepo()
	events := &fakeOutbox{}

	signer, err := security.NewTokenSigner(security.SignerOptions{
		Secret:   "a-test-secret-that-is-long-enough-to-pass",
		Issuer:   "denisco_backend",
		Audience: "denisco_api",
		TTL:      15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenSigner() returned error: %v", err)
	}

	authCfg := config.AuthConfig{
		RefreshTTL:        720 * time.Hour,
		BcryptCost:        10,
		PasswordResetTTL:  time.Hour,
		PasswordResetMax:  3,
		PasswordResetWin:  time.Hour,
		PasswordMinLength: 8,
		PasswordMaxLength: 72,
		ExposeResetToken:  true,
	}

	appCfg := config.AppConfig{Env: config.EnvTest, WebURL: "http://localhost:3000"}

	service := NewService(users, refresh, resets, events, security.NewHasher(10), signer, authCfg, appCfg)

	return &harness{service: service, users: users, refresh: refresh, resets: resets, events: events}
}

// register creates the standard test customer.
func (h *harness) register(t *testing.T) *Session {
	t.Helper()

	session, err := h.service.Register(context.Background(), RegisterInput{
		FirstName: "Ada",
		LastName:  "Obi",
		Email:     "Ada@Denisco.com",
		Phone:     "+2348012345678",
		Password:  "farm-secret-1",
	})
	if err != nil {
		t.Fatalf("Register() returned error: %v", err)
	}
	return session
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegisterCreatesActiveCustomer(t *testing.T) {
	h := newHarness(t)

	session := h.register(t)

	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("Register() did not issue a session")
	}
	if session.User.Email != "ada@denisco.com" {
		t.Errorf("email = %q, want the normalized address", session.User.Email)
	}
	if session.User.Role != usersdomain.RoleCustomer {
		t.Errorf("role = %q, want %q", session.User.Role, usersdomain.RoleCustomer)
	}
	if session.User.Status != usersdomain.StatusActive {
		t.Errorf("status = %q, want %q", session.User.Status, usersdomain.StatusActive)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	_, err := h.service.Register(context.Background(), RegisterInput{
		FirstName: "Ada",
		LastName:  "Obi",
		Email:     "ada@denisco.com",
		Phone:     "+2348012345678",
		Password:  "farm-secret-1",
	})
	if !errors.Is(err, usersdomain.ErrEmailTaken) {
		t.Errorf("Register() error = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	h := newHarness(t)

	_, err := h.service.Register(context.Background(), RegisterInput{
		FirstName: "Ada",
		LastName:  "Obi",
		Email:     "ada@denisco.com",
		Phone:     "+2348012345678",
		Password:  "short",
	})
	if err == nil {
		t.Error("Register() accepted a password below the minimum length")
	}
}

func TestLoginSucceedsAndRejectsBadCredentials(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	if _, err := h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "farm-secret-1"}); err != nil {
		t.Fatalf("Login() returned error: %v", err)
	}

	_, err := h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "wrong"})
	if !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}

	// An unknown address must fail identically, so the endpoint cannot be used
	// to discover registered emails.
	_, err = h.service.Login(context.Background(), LoginInput{Email: "nobody@denisco.com", Password: "farm-secret-1"})
	if !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() error for unknown email = %v, want ErrInvalidCredentials", err)
	}
}

func TestAdminLoginRejectsCustomer(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	_, err := h.service.AdminLogin(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "farm-secret-1"})
	if !errors.Is(err, authdomain.ErrAdminOnly) {
		t.Errorf("AdminLogin() error = %v, want ErrAdminOnly", err)
	}
}

func TestAdminLoginAcceptsAdmin(t *testing.T) {
	h := newHarness(t)

	hasher := security.NewHasher(10)
	hash, err := hasher.Hash("admin-secret-1")
	if err != nil {
		t.Fatalf("Hash() returned error: %v", err)
	}

	now := time.Now().UTC()
	admin := &usersdomain.User{
		ID:           bson.NewObjectID(),
		FirstName:    "Denisco",
		LastName:     "Admin",
		Email:        "admin@denisco.com",
		Phone:        "+2348000000000",
		PasswordHash: hash,
		Role:         usersdomain.RoleSuperAdmin,
		Status:       usersdomain.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.users.Create(context.Background(), admin); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	session, err := h.service.AdminLogin(context.Background(), LoginInput{Email: "admin@denisco.com", Password: "admin-secret-1"})
	if err != nil {
		t.Fatalf("AdminLogin() returned error: %v", err)
	}
	if session.User.Role != usersdomain.RoleSuperAdmin {
		t.Errorf("role = %q, want %q", session.User.Role, usersdomain.RoleSuperAdmin)
	}
}

func TestLoginRejectsSuspendedAccount(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	id, err := bson.ObjectIDFromHex(session.User.ID)
	if err != nil {
		t.Fatalf("ObjectIDFromHex() returned error: %v", err)
	}
	h.users.byID[id.Hex()].Status = usersdomain.StatusSuspended

	_, err = h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "farm-secret-1"})
	if !errors.Is(err, usersdomain.ErrUserSuspended) {
		t.Errorf("Login() error = %v, want ErrUserSuspended", err)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	rotated, err := h.service.Refresh(context.Background(), session.RefreshToken, "test-agent")
	if err != nil {
		t.Fatalf("Refresh() returned error: %v", err)
	}

	if rotated.RefreshToken == session.RefreshToken {
		t.Error("Refresh() returned the same refresh token; it must rotate")
	}
	if rotated.AccessToken == "" {
		t.Error("Refresh() did not issue an access token")
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	rotated, err := h.service.Refresh(context.Background(), session.RefreshToken, "test-agent")
	if err != nil {
		t.Fatalf("Refresh() returned error: %v", err)
	}

	// Replaying the consumed token must fail and end the whole session.
	if _, err := h.service.Refresh(context.Background(), session.RefreshToken, "test-agent"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Errorf("Refresh() error on reuse = %v, want ErrInvalidRefresh", err)
	}

	if _, err := h.service.Refresh(context.Background(), rotated.RefreshToken, "test-agent"); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Error("the successor token still works after reuse was detected; the family was not revoked")
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	h := newHarness(t)

	if _, err := h.service.Refresh(context.Background(), "not-a-real-token", ""); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Errorf("Refresh() error = %v, want ErrInvalidRefresh", err)
	}

	if _, err := h.service.Refresh(context.Background(), "", ""); !errors.Is(err, authdomain.ErrMissingRefresh) {
		t.Errorf("Refresh() error for empty token = %v, want ErrMissingRefresh", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	if err := h.service.Logout(context.Background(), session.RefreshToken); err != nil {
		t.Fatalf("Logout() returned error: %v", err)
	}

	if _, err := h.service.Refresh(context.Background(), session.RefreshToken, ""); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Error("the refresh token still works after logout")
	}

	// Logging out twice, or without a session, is not an error.
	if err := h.service.Logout(context.Background(), session.RefreshToken); err != nil {
		t.Errorf("Logout() on an already-revoked session returned error: %v", err)
	}
	if err := h.service.Logout(context.Background(), ""); err != nil {
		t.Errorf("Logout() without a cookie returned error: %v", err)
	}
}

func TestForgotPasswordIsSilentForUnknownEmail(t *testing.T) {
	h := newHarness(t)

	token, err := h.service.ForgotPassword(context.Background(), "nobody@denisco.com")
	if err != nil {
		t.Fatalf("ForgotPassword() returned error: %v", err)
	}
	if token != "" {
		t.Error("ForgotPassword() issued a token for an unregistered address")
	}
	if len(h.events.events) != 0 {
		t.Errorf("events = %v, want none for an unregistered address", h.events.events)
	}
}

func TestForgotPasswordThrottlesPerAccount(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	for i := 0; i < 3; i++ {
		if _, err := h.service.ForgotPassword(context.Background(), "ada@denisco.com"); err != nil {
			t.Fatalf("ForgotPassword() attempt %d returned error: %v", i+1, err)
		}
	}

	if _, err := h.service.ForgotPassword(context.Background(), "ada@denisco.com"); !errors.Is(err, authdomain.ErrResetThrottled) {
		t.Errorf("ForgotPassword() error = %v, want ErrResetThrottled", err)
	}
}

func TestResetPasswordIsSingleUseAndRevokesSessions(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	token, err := h.service.ForgotPassword(context.Background(), "ada@denisco.com")
	if err != nil {
		t.Fatalf("ForgotPassword() returned error: %v", err)
	}
	if token == "" {
		t.Fatal("ForgotPassword() did not expose a token; the test harness enables it")
	}

	if err := h.service.ResetPassword(context.Background(), ResetPasswordInput{Token: token, Password: "new-farm-secret"}); err != nil {
		t.Fatalf("ResetPassword() returned error: %v", err)
	}

	// The old password must no longer work and the new one must.
	if _, err := h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "farm-secret-1"}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Error("the old password still authenticates after a reset")
	}
	if _, err := h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "new-farm-secret"}); err != nil {
		t.Errorf("Login() with the new password returned error: %v", err)
	}

	// Sessions issued before the reset must be dead.
	if _, err := h.service.Refresh(context.Background(), session.RefreshToken, ""); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Error("a pre-reset session survived the password reset")
	}

	// The token is single-use.
	if err := h.service.ResetPassword(context.Background(), ResetPasswordInput{Token: token, Password: "another-secret-1"}); !errors.Is(err, authdomain.ErrInvalidResetToken) {
		t.Error("the reset token was accepted twice")
	}
}

func TestResetPasswordRejectsExpiredToken(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	token, err := h.service.ForgotPassword(context.Background(), "ada@denisco.com")
	if err != nil {
		t.Fatalf("ForgotPassword() returned error: %v", err)
	}

	// Age the record past its expiry.
	record := h.resets.byHash[security.HashToken(token)]
	record.ExpiresAt = time.Now().UTC().Add(-time.Minute)

	if err := h.service.ResetPassword(context.Background(), ResetPasswordInput{Token: token, Password: "new-farm-secret"}); !errors.Is(err, authdomain.ErrInvalidResetToken) {
		t.Errorf("ResetPassword() error = %v, want ErrInvalidResetToken", err)
	}
}

func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	// The current password must be re-verified.
	err := h.service.ChangePassword(context.Background(), session.User.ID, ChangePasswordInput{
		CurrentPassword: "wrong",
		NewPassword:     "another-secret-1",
	})
	if !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("ChangePassword() error = %v, want ErrInvalidCredentials", err)
	}

	// Reusing the same password is rejected.
	err = h.service.ChangePassword(context.Background(), session.User.ID, ChangePasswordInput{
		CurrentPassword: "farm-secret-1",
		NewPassword:     "farm-secret-1",
	})
	if !errors.Is(err, authdomain.ErrSamePassword) {
		t.Errorf("ChangePassword() error = %v, want ErrSamePassword", err)
	}

	if err := h.service.ChangePassword(context.Background(), session.User.ID, ChangePasswordInput{
		CurrentPassword: "farm-secret-1",
		NewPassword:     "another-secret-1",
	}); err != nil {
		t.Fatalf("ChangePassword() returned error: %v", err)
	}

	if _, err := h.service.Login(context.Background(), LoginInput{Email: "ada@denisco.com", Password: "another-secret-1"}); err != nil {
		t.Errorf("Login() with the new password returned error: %v", err)
	}

	if _, err := h.service.Refresh(context.Background(), session.RefreshToken, ""); !errors.Is(err, authdomain.ErrInvalidRefresh) {
		t.Error("a pre-change session survived the password change")
	}
}

func TestCurrentUser(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	view, err := h.service.CurrentUser(context.Background(), session.User.ID)
	if err != nil {
		t.Fatalf("CurrentUser() returned error: %v", err)
	}
	if view.Email != "ada@denisco.com" {
		t.Errorf("email = %q, want the registered address", view.Email)
	}

	if _, err := h.service.CurrentUser(context.Background(), "not-an-object-id"); err == nil {
		t.Error("CurrentUser() accepted a malformed user id")
	}

	// A suspended account loses access before its access token expires.
	id, _ := bson.ObjectIDFromHex(session.User.ID)
	h.users.byID[id.Hex()].Status = usersdomain.StatusSuspended

	if _, err := h.service.CurrentUser(context.Background(), session.User.ID); !errors.Is(err, usersdomain.ErrUserSuspended) {
		t.Errorf("CurrentUser() error = %v, want ErrUserSuspended", err)
	}
}

func TestRegisterEmitsWelcomeEvent(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	if len(h.events.events) != 1 || h.events.events[0] != "user.registered" {
		t.Errorf("events = %v, want [user.registered]", h.events.events)
	}
}
