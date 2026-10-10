// Command seed-admin creates the first administrative account.
//
// Administrative roles are never grantable through the public API, so the
// initial admin is provisioned here. The command is idempotent: an existing
// account is left untouched unless -reset-password is passed.
//
// Usage:
//
//	go run ./scripts/seed-admin -email admin@denisco.com -first Denisco -last Admin
//
// The password is read from ADMIN_PASSWORD so it never lands in shell history.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	usersinfra "github.com/Dev-folabi/denisco_backend/internal/modules/users/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func main() {
	var (
		email         = flag.String("email", "", "administrator email address (required)")
		firstName     = flag.String("first", "Denisco", "administrator first name")
		lastName      = flag.String("last", "Administrator", "administrator last name")
		phone         = flag.String("phone", "+2348000000000", "administrator phone number")
		role          = flag.String("role", usersdomain.RoleSuperAdmin, "role: admin or super_admin")
		resetPassword = flag.Bool("reset-password", false, "overwrite the password of an existing account")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("failed to load configuration", err)
	}

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)

	if strings.TrimSpace(*email) == "" {
		fail("missing flag", errors.New("-email is required"))
	}
	if *role != usersdomain.RoleAdmin && *role != usersdomain.RoleSuperAdmin {
		fail("invalid flag", fmt.Errorf("-role must be %q or %q", usersdomain.RoleAdmin, usersdomain.RoleSuperAdmin))
	}

	password := os.Getenv("ADMIN_PASSWORD")
	if len(password) < cfg.Auth.PasswordMinLength {
		fail("invalid password", fmt.Errorf("ADMIN_PASSWORD must be set and at least %d characters", cfg.Auth.PasswordMinLength))
	}

	normalizedEmail, err := usersdomain.ValidateEmail(*email)
	if err != nil {
		fail("invalid email", err)
	}

	normalizedPhone, err := usersdomain.ValidatePhone(*phone)
	if err != nil {
		fail("invalid phone", err)
	}

	client, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		fail("failed to connect to mongodb", err)
	}
	defer func() { _ = database.Disconnect(client) }()

	db := client.Database(cfg.Mongo.Database)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := database.EnsureIndexes(ctx, db); err != nil {
		fail("failed to create indexes", err)
	}

	repo := usersinfra.NewMongoRepository(db)
	hasher := security.NewHasher(cfg.Auth.BcryptCost)

	hash, err := hasher.Hash(password)
	if err != nil {
		fail("failed to hash password", err)
	}

	existing, err := repo.FindByEmail(ctx, normalizedEmail)
	switch {
	case err == nil && !*resetPassword:
		log.Info("administrator already exists; pass -reset-password to overwrite the password",
			slog.String("email", normalizedEmail),
			slog.String("role", existing.Role),
		)
		return

	case err == nil:
		if err := repo.UpdatePasswordHash(ctx, existing.ID, hash); err != nil {
			fail("failed to update password", err)
		}
		log.Info("administrator password updated", slog.String("email", normalizedEmail))
		return

	case !errors.Is(err, usersdomain.ErrUserNotFound):
		fail("failed to look up administrator", err)
	}

	now := time.Now().UTC()
	user := &usersdomain.User{
		ID:            bson.NewObjectID(),
		FirstName:     *firstName,
		LastName:      *lastName,
		Email:         normalizedEmail,
		Phone:         normalizedPhone,
		PasswordHash:  hash,
		Role:          *role,
		Status:        usersdomain.StatusActive,
		EmailVerified: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := repo.Create(ctx, user); err != nil {
		fail("failed to create administrator", err)
	}

	log.Info("administrator created",
		slog.String("email", user.Email),
		slog.String("role", user.Role),
		slog.String("user_id", user.ID.Hex()),
	)
}

// fail reports a fatal setup error and exits non-zero.
func fail(message string, err error) {
	slog.Error(message, slog.String("error", err.Error()))
	os.Exit(1)
}
