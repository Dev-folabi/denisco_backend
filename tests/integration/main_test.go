// Package integration exercises the backend against a real MongoDB
// deployment: the repositories, the multi-document transactions behind
// checkout and payment, the authentication flow and the concurrency rules
// that only a replica set can prove.
//
// The suite skips itself unless a MongoDB URI is configured, so `go test ./...`
// stays runnable without a database. Point it at a throwaway database:
//
//	MONGODB_TEST_URI=mongodb+srv://… MONGODB_TEST_DATABASE=denisco_integration_test \
//	    go test ./tests/integration/
//
// Transactions require a replica set, which MongoDB Atlas and `mongod
// --replSet` both provide; a standalone server cannot run this suite.
package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/bootstrap"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// defaultTestDatabase is where the suite writes unless told otherwise. It is
// deliberately not a name any environment would use for real data.
const defaultTestDatabase = "denisco_integration_test"

// suite is built once for the whole package: connecting to Atlas and creating
// indexes is slow enough that doing it per test would dominate the run.
var suite *environment

// skipReason explains why the suite is unavailable, shown on every skip.
var skipReason string

// environment is the wired application under test.
type environment struct {
	container *bootstrap.Container
	db        *mongo.Database
	provider  *paystackStub
	// api is the fully wired HTTP handler, used where a test must go through
	// the transport layer — the signed Paystack webhook, for one.
	api http.Handler
}

func TestMain(m *testing.M) {
	// The backend's own .env is the convenient source of a developer's Atlas
	// URI. Real environment variables win, so CI can override it.
	_ = godotenv.Load("../../.env")

	env, err := newEnvironment()
	switch {
	case err != nil:
		skipReason = err.Error()
	default:
		suite = env
	}

	code := m.Run()

	if suite != nil {
		suite.provider.Close()
		suite.container.Close()
	}

	os.Exit(code)
}

// newEnvironment connects to the test database and wires every module exactly
// as the API process does, so the tests exercise production wiring rather than
// a parallel assembly of their own.
func newEnvironment() (*environment, error) {
	uri := firstNonEmpty(os.Getenv("MONGODB_TEST_URI"), os.Getenv("MONGODB_URI"))
	if uri == "" {
		return nil, fmt.Errorf("integration tests need MONGODB_TEST_URI (or MONGODB_URI)")
	}

	dbName := firstNonEmpty(os.Getenv("MONGODB_TEST_DATABASE"), defaultTestDatabase)

	// The suite empties every collection between tests, so pointing it at the
	// development or production database would destroy real data.
	if live := strings.TrimSpace(os.Getenv("MONGODB_DATABASE")); live != "" && live == dbName {
		return nil, fmt.Errorf("refusing to run: MONGODB_TEST_DATABASE (%q) is the configured application database", dbName)
	}

	provider := newPaystackStub()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	container, err := bootstrap.NewContainer(ctx, testConfig(uri, dbName, provider.URL()), quietLogger())
	if err != nil {
		provider.Close()
		return nil, fmt.Errorf("could not reach MongoDB at the configured URI: %w", err)
	}

	return &environment{
		container: container,
		db:        container.Database,
		provider:  provider,
		api:       bootstrap.NewAPIHandler(container),
	}, nil
}

// testConfig builds the configuration the suite runs under. It is written out
// in full rather than read from the environment so a stray variable cannot
// change what the tests assert.
func testConfig(uri, dbName, providerURL string) config.Config {
	return config.Config{
		App: config.AppConfig{
			Env:     config.EnvTest,
			Name:    "denisco-api-test",
			Port:    0,
			BaseURL: "http://localhost:4000",
			WebURL:  "http://localhost:3000",
		},
		HTTP: config.HTTPConfig{
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    15 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 10 * time.Second,
			MaxBodyBytes:    1 << 20,
		},
		CORS: config.CORSConfig{
			WebOrigin:   "http://localhost:3000",
			AdminOrigin: "http://localhost:3001",
		},
		Mongo: config.MongoConfig{URI: uri, Database: dbName, MaxPoolSize: 20},
		Redis: config.RedisConfig{URL: os.Getenv("REDIS_URL")},
		JWT: config.JWTConfig{
			Secret:    "an-integration-test-secret-long-enough-to-seed-ed25519",
			Issuer:    "denisco_backend",
			Audience:  "denisco_api",
			AccessTTL: 15 * time.Minute,
		},
		Auth: config.AuthConfig{
			RefreshTTL: 720 * time.Hour,
			// Cost 10 rather than production's 12: the suite hashes dozens of
			// passwords and bcrypt is deliberately slow.
			BcryptCost:        10,
			PasswordResetTTL:  time.Hour,
			PasswordResetMax:  3,
			PasswordResetWin:  time.Hour,
			RefreshCookieName: "denisco_refresh",
			RefreshCookiePath: "/api/v1/auth",
			PasswordMinLength: 8,
			PasswordMaxLength: 72,
			ExposeResetToken:  true,
		},
		// Throttling would fail the concurrency tests for the wrong reason.
		RateLimit: config.RateLimitConfig{Enabled: false},
		Log:       config.LogConfig{Level: "error"},
		Paystack:  config.PaystackConfig{SecretKey: stubSecretKey, BaseURL: providerURL},
		Worker:    config.WorkerConfig{Concurrency: 2, ReservationExpiry: 45 * time.Minute},
	}
}

// quietLogger discards application logs: the suite deliberately provokes
// errors, and their log lines would bury the test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// require returns the suite with an empty database, or skips the test when no
// MongoDB deployment is configured.
func require(t *testing.T) *environment {
	t.Helper()

	if suite == nil {
		t.Skip("skipping integration test: " + skipReason)
	}

	suite.truncate(t)
	return suite
}

// collections is every collection the suite writes to. Documents are deleted
// rather than the collections dropped, so the indexes created at startup —
// which several tests depend on — survive between cases.
var collections = []string{
	database.CollUsers,
	database.CollRefreshTokens,
	database.CollPasswordResets,
	database.CollProducts,
	database.CollCategories,
	database.CollCarts,
	database.CollOrders,
	database.CollPayments,
	database.CollInventory,
	database.CollInventoryMovements,
	database.CollConsultationTypes,
	database.CollConsultationSlots,
	database.CollConsultationBookings,
	database.CollDeliveryLocations,
	database.CollSiteSettings,
	database.CollOutboxEvents,
	database.CollAuditLogs,
}

// truncate empties the database so each test starts from a known state.
func (e *environment) truncate(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, name := range collections {
		if _, err := e.db.Collection(name).DeleteMany(ctx, bson.M{}); err != nil {
			t.Fatalf("could not empty %s: %v", name, err)
		}
	}

	e.provider.reset()
}

// ctx returns a context bounded for one test.
func ctx(t *testing.T) context.Context {
	t.Helper()

	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	return c
}

// firstNonEmpty returns the first value that carries something.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
