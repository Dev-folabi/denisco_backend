package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	authdomain "github.com/Dev-folabi/denisco_backend/internal/modules/auth/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// dummyHash is a valid bcrypt hash of a random value. When an email is not
// registered, login still verifies against it so the response time does not
// reveal whether the account exists.
const dummyHash = "$2a$12$c6kZQ5zkQcCDN6IGQqz9MO1vW5Hq1w1ZQ0q9ULO3vkVZ2uJ8HMPOq"

// Service implements the authentication workflows.
type Service struct {
	users   usersdomain.Repository
	refresh authdomain.RefreshTokenRepository
	resets  authdomain.PasswordResetRepository
	events  outbox.Writer
	hasher  *security.Hasher
	signer  *security.TokenSigner
	authCfg config.AuthConfig
	appCfg  config.AppConfig
}

// NewService wires the dependencies the workflows need.
func NewService(
	users usersdomain.Repository,
	refresh authdomain.RefreshTokenRepository,
	resets authdomain.PasswordResetRepository,
	events outbox.Writer,
	hasher *security.Hasher,
	signer *security.TokenSigner,
	authCfg config.AuthConfig,
	appCfg config.AppConfig,
) *Service {
	return &Service{
		users:   users,
		refresh: refresh,
		resets:  resets,
		events:  events,
		hasher:  hasher,
		signer:  signer,
		authCfg: authCfg,
		appCfg:  appCfg,
	}
}

// RefreshTTL exposes the refresh lifetime so the transport layer can set a
// matching cookie expiry.
func (s *Service) RefreshTTL() time.Duration { return s.authCfg.RefreshTTL }

// Register creates a customer account and signs the new user in. Accounts
// created here are always customers: administrative roles are granted out of
// band, never through a public endpoint.
func (s *Service) Register(ctx context.Context, input RegisterInput) (*Session, error) {
	firstName, err := usersdomain.ValidateName("first_name", input.FirstName)
	if err != nil {
		return nil, err
	}

	lastName, err := usersdomain.ValidateName("last_name", input.LastName)
	if err != nil {
		return nil, err
	}

	email, err := usersdomain.ValidateEmail(input.Email)
	if err != nil {
		return nil, err
	}

	phone, err := usersdomain.ValidatePhone(input.Phone)
	if err != nil {
		return nil, err
	}

	if err := validateNewPassword(input.Password, s.authCfg.PasswordMinLength, s.authCfg.PasswordMaxLength); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	user := &usersdomain.User{
		ID:            bson.NewObjectID(),
		FirstName:     firstName,
		LastName:      lastName,
		Email:         email,
		Phone:         phone,
		PasswordHash:  hash,
		Role:          usersdomain.RoleCustomer,
		Status:        usersdomain.StatusActive,
		EmailVerified: false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	// Welcome email is delivered by the worker; a failure to record the event
	// must not fail registration.
	s.emit(ctx, outbox.TypeUserRegistered, user.ID.Hex(), map[string]any{
		"user_id":    user.ID.Hex(),
		"email":      user.Email,
		"first_name": user.FirstName,
	})

	logger.FromContext(ctx).Info("user registered",
		slog.String("module", "auth"),
		slog.String("operation", "register"),
		slog.String("user_id", user.ID.Hex()),
	)

	return s.issueSession(ctx, user, "")
}

// Login authenticates a customer or admin from the customer-facing app.
func (s *Service) Login(ctx context.Context, input LoginInput) (*Session, error) {
	return s.authenticate(ctx, input, false)
}

// AdminLogin authenticates against the admin dashboard and additionally
// requires an administrative role, so a customer account cannot sign in there
// even with valid credentials.
func (s *Service) AdminLogin(ctx context.Context, input LoginInput) (*Session, error) {
	return s.authenticate(ctx, input, true)
}

// authenticate verifies credentials and account status, then issues a session.
func (s *Service) authenticate(ctx context.Context, input LoginInput, adminOnly bool) (*Session, error) {
	email := usersdomain.NormalizeEmail(input.Email)
	if email == "" || input.Password == "" {
		return nil, authdomain.ErrInvalidCredentials
	}

	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, usersdomain.ErrUserNotFound) {
			// Spend comparable time so timing does not leak account existence.
			s.hasher.Verify(dummyHash, input.Password)
			return nil, authdomain.ErrInvalidCredentials
		}
		return nil, err
	}

	if !s.hasher.Verify(user.PasswordHash, input.Password) {
		logger.FromContext(ctx).Warn("authentication failed",
			slog.String("module", "auth"),
			slog.String("operation", "login"),
			slog.String("reason", "password mismatch"),
		)
		return nil, authdomain.ErrInvalidCredentials
	}

	if err := user.EnsureActive(); err != nil {
		return nil, err
	}

	if adminOnly && !user.IsAdmin() {
		return nil, authdomain.ErrAdminOnly
	}

	// Upgrade the stored hash when the configured cost has changed.
	if s.hasher.NeedsRehash(user.PasswordHash) {
		if hash, err := s.hasher.Hash(input.Password); err == nil {
			if err := s.users.UpdatePasswordHash(ctx, user.ID, hash); err != nil {
				logger.FromContext(ctx).Warn("password rehash failed", slog.String("user_id", user.ID.Hex()))
			}
		}
	}

	now := time.Now().UTC()
	if err := s.users.TouchLastLogin(ctx, user.ID, now); err != nil {
		logger.FromContext(ctx).Warn("could not record last login", slog.String("user_id", user.ID.Hex()))
	}

	logger.FromContext(ctx).Info("user authenticated",
		slog.String("module", "auth"),
		slog.String("operation", "login"),
		slog.String("user_id", user.ID.Hex()),
		slog.String("role", user.Role),
	)

	return s.issueSession(ctx, user, input.UserAgent)
}

// Refresh rotates a refresh token and issues a new access token.
//
// Reuse detection: the token record is consumed with a conditional update. If
// the update matches nothing, the token was already consumed (or revoked), so
// the whole family is revoked and the caller must sign in again.
func (s *Service) Refresh(ctx context.Context, token, userAgent string) (*Session, error) {
	if token == "" {
		return nil, authdomain.ErrMissingRefresh
	}

	hash := security.HashToken(token)

	record, err := s.refresh.FindByHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if !record.IsUsable(now) {
		if record.Revoked {
			// A revoked token being presented means it leaked or was replayed.
			s.handleReuse(ctx, record)
		}
		return nil, authdomain.ErrInvalidRefresh
	}

	user, err := s.users.FindByID(ctx, record.UserID)
	if err != nil {
		return nil, authdomain.ErrInvalidRefresh
	}

	if err := user.EnsureActive(); err != nil {
		return nil, err
	}

	successor, err := security.NewOpaqueToken()
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}
	successorHash := security.HashToken(successor)

	rotated, err := s.refresh.Rotate(ctx, hash, successorHash)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}
	if !rotated {
		// Another request consumed this token first: treat it as reuse.
		s.handleReuse(ctx, record)
		return nil, authdomain.ErrInvalidRefresh
	}

	next := &authdomain.RefreshToken{
		UserID:    user.ID,
		TokenHash: successorHash,
		FamilyID:  record.FamilyID,
		ExpiresAt: now.Add(s.authCfg.RefreshTTL),
		UserAgent: userAgent,
		CreatedAt: now,
	}
	if err := s.refresh.Create(ctx, next); err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	accessToken, expiresAt, err := s.signer.Sign(user.ID.Hex(), user.Role)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	return &Session{
		AccessToken:      accessToken,
		AccessExpiresAt:  expiresAt,
		AccessExpiresIn:  int(s.signer.TTL().Seconds()),
		RefreshToken:     successor,
		RefreshExpiresAt: next.ExpiresAt,
		User:             newUserView(user),
	}, nil
}

// Logout revokes the presented session. A missing or unknown token is not an
// error: the caller's intent (no active session) is already satisfied.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	record, err := s.refresh.FindByHash(ctx, security.HashToken(token))
	if err != nil {
		if errors.Is(err, authdomain.ErrInvalidRefresh) {
			return nil
		}
		return err
	}

	if err := s.refresh.RevokeFamily(ctx, record.FamilyID); err != nil {
		return apperror.Internal.Wrap(err)
	}

	logger.FromContext(ctx).Info("session revoked",
		slog.String("module", "auth"),
		slog.String("operation", "logout"),
		slog.String("user_id", record.UserID.Hex()),
	)

	return nil
}

// ForgotPassword issues a reset token when the account exists. The caller
// receives the same response either way, so the endpoint cannot be used to
// discover registered email addresses.
//
// The returned token is non-empty only when AUTH_EXPOSE_RESET_TOKEN is on,
// which is a development aid and is ignored in production.
func (s *Service) ForgotPassword(ctx context.Context, email string) (string, error) {
	normalized := usersdomain.NormalizeEmail(email)
	if normalized == "" {
		return "", apperror.Validation.WithDetails(map[string]any{"email": "is required"})
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, usersdomain.ErrUserNotFound) {
			return "", nil
		}
		return "", err
	}

	// Throttle per account, not only per IP, so one address cannot be spammed
	// from a rotating set of clients.
	since := time.Now().UTC().Add(-s.authCfg.PasswordResetWin)
	count, err := s.resets.CountSince(ctx, user.ID, since)
	if err != nil {
		return "", apperror.Internal.Wrap(err)
	}
	if count >= int64(s.authCfg.PasswordResetMax) {
		return "", authdomain.ErrResetThrottled
	}

	token, err := security.NewOpaqueToken()
	if err != nil {
		return "", apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	reset := &authdomain.PasswordReset{
		UserID:    user.ID,
		TokenHash: security.HashToken(token),
		ExpiresAt: now.Add(s.authCfg.PasswordResetTTL),
		CreatedAt: now,
	}
	if err := s.resets.Create(ctx, reset); err != nil {
		return "", apperror.Internal.Wrap(err)
	}

	// The plaintext token travels to the customer only by email, through the
	// outbox. It is never logged.
	s.emit(ctx, outbox.TypePasswordResetRequested, user.ID.Hex(), map[string]any{
		"user_id":    user.ID.Hex(),
		"email":      user.Email,
		"first_name": user.FirstName,
		"reset_url":  s.appCfg.WebURL + "/reset-password?token=" + token,
		"expires_at": reset.ExpiresAt.Format(time.RFC3339),
	})

	logger.FromContext(ctx).Info("password reset requested",
		slog.String("module", "auth"),
		slog.String("operation", "forgot_password"),
		slog.String("user_id", user.ID.Hex()),
	)

	if s.authCfg.ExposeResetToken {
		return token, nil
	}
	return "", nil
}

// ResetPassword redeems a reset token and sets a new password. Every existing
// session is revoked, because a reset usually follows a suspected compromise.
func (s *Service) ResetPassword(ctx context.Context, input ResetPasswordInput) error {
	if input.Token == "" {
		return authdomain.ErrInvalidResetToken
	}

	if err := validateNewPassword(input.Password, s.authCfg.PasswordMinLength, s.authCfg.PasswordMaxLength); err != nil {
		return err
	}

	hash := security.HashToken(input.Token)

	record, err := s.resets.FindByHash(ctx, hash)
	if err != nil {
		return err
	}
	if !record.IsUsable(time.Now().UTC()) {
		return authdomain.ErrInvalidResetToken
	}

	user, err := s.users.FindByID(ctx, record.UserID)
	if err != nil {
		return authdomain.ErrInvalidResetToken
	}

	// Consume before writing the password so a replayed request cannot reset
	// the password twice.
	consumed, err := s.resets.Consume(ctx, hash)
	if err != nil {
		return apperror.Internal.Wrap(err)
	}
	if !consumed {
		return authdomain.ErrInvalidResetToken
	}

	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return apperror.Internal.Wrap(err)
	}

	if err := s.users.UpdatePasswordHash(ctx, user.ID, passwordHash); err != nil {
		return err
	}

	if err := s.refresh.RevokeAllForUser(ctx, user.ID); err != nil {
		return apperror.Internal.Wrap(err)
	}

	s.emit(ctx, outbox.TypePasswordChanged, user.ID.Hex(), map[string]any{
		"user_id": user.ID.Hex(),
		"email":   user.Email,
		"reason":  "reset",
	})

	logger.FromContext(ctx).Info("password reset completed",
		slog.String("module", "auth"),
		slog.String("operation", "reset_password"),
		slog.String("user_id", user.ID.Hex()),
	)

	return nil
}

// ChangePassword updates the signed-in user's password after re-verifying the
// current one, and revokes other sessions.
func (s *Service) ChangePassword(ctx context.Context, userID string, input ChangePasswordInput) error {
	id, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return apperror.Unauthorized
	}

	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if !s.hasher.Verify(user.PasswordHash, input.CurrentPassword) {
		return authdomain.ErrInvalidCredentials
	}

	if err := validateNewPassword(input.NewPassword, s.authCfg.PasswordMinLength, s.authCfg.PasswordMaxLength); err != nil {
		return err
	}

	if input.CurrentPassword == input.NewPassword {
		return authdomain.ErrSamePassword
	}

	hash, err := s.hasher.Hash(input.NewPassword)
	if err != nil {
		return apperror.Internal.Wrap(err)
	}

	if err := s.users.UpdatePasswordHash(ctx, user.ID, hash); err != nil {
		return err
	}

	if err := s.refresh.RevokeAllForUser(ctx, user.ID); err != nil {
		return apperror.Internal.Wrap(err)
	}

	s.emit(ctx, outbox.TypePasswordChanged, user.ID.Hex(), map[string]any{
		"user_id": user.ID.Hex(),
		"email":   user.Email,
		"reason":  "change",
	})

	return nil
}

// CurrentUser returns the authenticated user's profile, re-reading the account
// so a suspended user loses access before their access token expires.
func (s *Service) CurrentUser(ctx context.Context, userID string) (*UserView, error) {
	id, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperror.Unauthorized
	}

	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := user.EnsureActive(); err != nil {
		return nil, err
	}

	view := newUserView(user)
	return &view, nil
}

// issueSession mints an access token and opens a new refresh-token family.
func (s *Service) issueSession(ctx context.Context, user *usersdomain.User, userAgent string) (*Session, error) {
	accessToken, expiresAt, err := s.signer.Sign(user.ID.Hex(), user.Role)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	refreshToken, err := security.NewOpaqueToken()
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	familyID, err := security.NewID()
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	record := &authdomain.RefreshToken{
		UserID:    user.ID,
		TokenHash: security.HashToken(refreshToken),
		FamilyID:  familyID,
		ExpiresAt: now.Add(s.authCfg.RefreshTTL),
		UserAgent: userAgent,
		CreatedAt: now,
	}
	if err := s.refresh.Create(ctx, record); err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	return &Session{
		AccessToken:      accessToken,
		AccessExpiresAt:  expiresAt,
		AccessExpiresIn:  int(s.signer.TTL().Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: record.ExpiresAt,
		User:             newUserView(user),
	}, nil
}

// handleReuse revokes a compromised token family and records the event.
func (s *Service) handleReuse(ctx context.Context, record *authdomain.RefreshToken) {
	log := logger.FromContext(ctx)
	log.Warn("refresh token reuse detected",
		slog.String("module", "auth"),
		slog.String("operation", "refresh"),
		slog.String("user_id", record.UserID.Hex()),
		slog.String("family_id", record.FamilyID),
	)

	if err := s.refresh.RevokeFamily(ctx, record.FamilyID); err != nil {
		log.Error("could not revoke token family", slog.String("family_id", record.FamilyID))
	}
}

// emit records a domain event, logging rather than failing the request when
// the append fails: the user-visible operation has already succeeded.
func (s *Service) emit(ctx context.Context, eventType, aggregateID string, payload map[string]any) {
	if s.events == nil {
		return
	}
	if err := s.events.Append(ctx, eventType, aggregateID, payload); err != nil {
		logger.FromContext(ctx).Error("could not append outbox event",
			slog.String("event_type", eventType),
			slog.String("aggregate_id", aggregateID),
			slog.String("error", err.Error()),
		)
	}
}
