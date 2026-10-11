// Package config loads and validates deployment configuration from the
// environment. The application fails fast when a mandatory setting is absent
// or invalid, so a misconfigured process never serves traffic.
package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environment names used for behaviour that must differ per deployment.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

type Config struct {
	App       AppConfig
	HTTP      HTTPConfig
	CORS      CORSConfig
	Mongo     MongoConfig
	Redis     RedisConfig
	JWT       JWTConfig
	Auth      AuthConfig
	RateLimit RateLimitConfig
	Log       LogConfig
	Paystack  PaystackConfig
	ImageKit  ImageKitConfig
	Resend    ResendConfig
	Worker    WorkerConfig
}

type AppConfig struct {
	Env     string
	Name    string
	Port    int
	BaseURL string
	WebURL  string
}

// IsProduction reports whether the process runs with production semantics
// (strict cookie flags, dev-only switches disabled).
func (a AppConfig) IsProduction() bool { return a.Env == EnvProduction }

type HTTPConfig struct {
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
	// ClientIPHeader names the header the deployment's reverse proxy sets
	// with the real client address, used for rate limiting and audit logs.
	// Empty — the default — trusts no header and uses the peer address, which
	// is the right behaviour when the API is reachable directly.
	ClientIPHeader string
}

type CORSConfig struct {
	WebOrigin   string
	AdminOrigin string
}

// AllowedOrigins returns the exact origins permitted to call the API with
// credentials. Used by both the CORS and CSRF middleware.
func (c CORSConfig) AllowedOrigins() []string {
	origins := make([]string, 0, 2)
	for _, origin := range []string{c.WebOrigin, c.AdminOrigin} {
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

type MongoConfig struct {
	URI         string
	Database    string
	MaxPoolSize uint64
}

type RedisConfig struct {
	URL string
}

type JWTConfig struct {
	// Secret seeds the Ed25519 signing key when no explicit key is supplied.
	Secret string
	// Ed25519Seed is a base64-encoded 32-byte seed. When set it takes
	// precedence over Secret and lets operators rotate keys explicitly.
	Ed25519Seed string
	Issuer      string
	Audience    string
	AccessTTL   time.Duration
}

type AuthConfig struct {
	RefreshTTL        time.Duration
	BcryptCost        int
	PasswordResetTTL  time.Duration
	PasswordResetMax  int           // requests allowed per window, per account
	PasswordResetWin  time.Duration // window for the above
	RefreshCookieName string
	RefreshCookiePath string
	CookieDomain      string
	CookieSecure      bool
	CookieSameSite    http.SameSite
	ExposeResetToken  bool // development aid; ignored in production
	PasswordMinLength int
	PasswordMaxLength int
}

type RateLimitConfig struct {
	Enabled       bool
	GeneralPerMin int
	LoginPerMin   int
	SignupPerHour int
	ResetPerHour  int
	// ContactPerHour bounds how many messages one address can send through
	// the contact form. It is the one anonymous endpoint that costs a human
	// a reading, so it gets a budget of its own rather than the general one.
	ContactPerHour int
}

type LogConfig struct {
	Level string
}

type PaystackConfig struct {
	SecretKey string
	BaseURL   string
}

type ImageKitConfig struct {
	PublicKey   string
	PrivateKey  string
	URLEndpoint string
}

type ResendConfig struct {
	APIKey    string
	EmailFrom string
	// AdminEmail receives a copy of consultation notifications so farm staff
	// see new appointments. Empty disables the copy.
	AdminEmail string
}

// WorkerConfig tunes the background job processor.
type WorkerConfig struct {
	// Concurrency is how many jobs run at once. The worker shares a 2-core
	// VPS with the API, so this stays small.
	Concurrency int
	// ReservationExpiry is how long an unpaid order holds its stock.
	ReservationExpiry time.Duration
}

// Load reads configuration from the process environment, falling back to a
// local .env file for development convenience.
func Load() (Config, error) {
	_ = godotenv.Load()

	var missing []string

	required := func(key string) string {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			missing = append(missing, key)
		}
		return value
	}

	env := getString("APP_ENV", EnvDevelopment)

	cfg := Config{
		App: AppConfig{
			Env:     env,
			Name:    getString("APP_NAME", "denisco-api"),
			Port:    getInt("API_PORT", 4000),
			BaseURL: getString("API_BASE_URL", "http://localhost:4000"),
			WebURL:  getString("WEB_ORIGIN", "http://localhost:3000"),
		},
		HTTP: HTTPConfig{
			ReadTimeout:     getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			MaxBodyBytes:    int64(getInt("HTTP_MAX_BODY_BYTES", 1<<20)),
			ClientIPHeader:  getString("CLIENT_IP_HEADER", ""),
		},
		CORS: CORSConfig{
			WebOrigin:   required("WEB_ORIGIN"),
			AdminOrigin: required("ADMIN_ORIGIN"),
		},
		Mongo: MongoConfig{
			URI:         required("MONGODB_URI"),
			Database:    getString("MONGODB_DATABASE", "denisco"),
			MaxPoolSize: uint64(getInt("MONGODB_MAX_POOL_SIZE", 20)),
		},
		Redis: RedisConfig{
			URL: getString("REDIS_URL", "redis://localhost:6379/0"),
		},
		JWT: JWTConfig{
			Secret:      required("JWT_ACCESS_SECRET"),
			Ed25519Seed: getString("JWT_ED25519_SEED", ""),
			Issuer:      getString("JWT_ISSUER", "denisco_backend"),
			Audience:    getString("JWT_AUDIENCE", "denisco_api"),
			AccessTTL:   getDuration("JWT_ACCESS_TTL", 15*time.Minute),
		},
		Auth: AuthConfig{
			RefreshTTL:        getDuration("REFRESH_TOKEN_TTL", 720*time.Hour),
			BcryptCost:        getInt("BCRYPT_COST", 12),
			PasswordResetTTL:  getDuration("PASSWORD_RESET_TTL", 1*time.Hour),
			PasswordResetMax:  getInt("PASSWORD_RESET_MAX_PER_WINDOW", 3),
			PasswordResetWin:  getDuration("PASSWORD_RESET_WINDOW", 1*time.Hour),
			RefreshCookieName: getString("REFRESH_COOKIE_NAME", "denisco_refresh"),
			RefreshCookiePath: getString("REFRESH_COOKIE_PATH", "/api/v1/auth"),
			CookieDomain:      getString("COOKIE_DOMAIN", ""),
			CookieSecure:      getBool("COOKIE_SECURE", env == EnvProduction),
			CookieSameSite:    parseSameSite(getString("COOKIE_SAMESITE", "lax")),
			ExposeResetToken:  getBool("AUTH_EXPOSE_RESET_TOKEN", false) && env != EnvProduction,
			PasswordMinLength: getInt("PASSWORD_MIN_LENGTH", 8),
			PasswordMaxLength: getInt("PASSWORD_MAX_LENGTH", 72), // bcrypt input limit
		},
		RateLimit: RateLimitConfig{
			GeneralPerMin:  getInt("RATE_LIMIT_GENERAL_PER_MIN", 120),
			LoginPerMin:    getInt("RATE_LIMIT_LOGIN_PER_MIN", 5),
			SignupPerHour:  getInt("RATE_LIMIT_SIGNUP_PER_HOUR", 10),
			ResetPerHour:   getInt("RATE_LIMIT_RESET_PER_HOUR", 3),
			ContactPerHour: getInt("RATE_LIMIT_CONTACT_PER_HOUR", 5),
		},
		Log: LogConfig{
			Level: getString("LOG_LEVEL", "info"),
		},
		Paystack: PaystackConfig{
			SecretKey: getString("PAYSTACK_SECRET_KEY", ""),
			BaseURL:   getString("PAYSTACK_BASE_URL", "https://api.paystack.co"),
		},
		ImageKit: ImageKitConfig{
			PublicKey:   getString("IMAGEKIT_PUBLIC_KEY", ""),
			PrivateKey:  getString("IMAGEKIT_PRIVATE_KEY", ""),
			URLEndpoint: getString("IMAGEKIT_URL_ENDPOINT", ""),
		},
		Resend: ResendConfig{
			APIKey:     getString("RESEND_API_KEY", ""),
			EmailFrom:  getString("EMAIL_FROM", ""),
			AdminEmail: getString("ADMIN_NOTIFICATION_EMAIL", ""),
		},
		Worker: WorkerConfig{
			Concurrency:       getInt("WORKER_CONCURRENCY", 5),
			ReservationExpiry: getDuration("RESERVATION_EXPIRY", 45*time.Minute),
		},
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// validate rejects values that are present but unusable, so the failure
// surfaces at startup rather than on the first request.
func (c Config) validate() error {
	if c.App.Port < 1 || c.App.Port > 65535 {
		return fmt.Errorf("API_PORT must be between 1 and 65535, got %d", c.App.Port)
	}
	if c.Auth.BcryptCost < 10 || c.Auth.BcryptCost > 31 {
		return fmt.Errorf("BCRYPT_COST must be between 10 and 31, got %d", c.Auth.BcryptCost)
	}
	if c.Auth.PasswordMinLength < 8 {
		return fmt.Errorf("PASSWORD_MIN_LENGTH must be at least 8, got %d", c.Auth.PasswordMinLength)
	}
	if c.Auth.PasswordMaxLength > 72 {
		return fmt.Errorf("PASSWORD_MAX_LENGTH must not exceed bcrypt's 72-byte limit, got %d", c.Auth.PasswordMaxLength)
	}
	if len(c.JWT.Secret) < 32 && c.JWT.Ed25519Seed == "" {
		return fmt.Errorf("JWT_ACCESS_SECRET must be at least 32 characters when JWT_ED25519_SEED is not set")
	}
	if c.JWT.AccessTTL <= 0 || c.JWT.AccessTTL > time.Hour {
		return fmt.Errorf("JWT_ACCESS_TTL must be greater than zero and at most 1h, got %s", c.JWT.AccessTTL)
	}
	if c.Auth.RefreshTTL < 24*time.Hour {
		return fmt.Errorf("REFRESH_TOKEN_TTL must be at least 24h, got %s", c.Auth.RefreshTTL)
	}
	if c.App.IsProduction() && !c.Auth.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true in production")
	}
	for _, origin := range c.CORS.AllowedOrigins() {
		if strings.Contains(origin, "*") {
			return fmt.Errorf("wildcard origins are not allowed, got %q", origin)
		}
	}
	// X-Forwarded-For carries a chain whose entries depend on how many proxies
	// appended to it, and anyone can send one. Trusting it would hand callers
	// the choice of which bucket the rate limiter counts them in, so only a
	// single-address header a proxy writes itself is accepted.
	if header := strings.ToLower(c.HTTP.ClientIPHeader); header == "x-forwarded-for" {
		return fmt.Errorf("CLIENT_IP_HEADER must name a single-address header such as CF-Connecting-IP or X-Real-IP, not X-Forwarded-For")
	}
	return nil
}

// getString reads a variable or falls back to a default.
func getString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// getInt reads an integer variable or falls back to a default.
func getInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// getBool reads a boolean variable or falls back to a default.
func getBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// getDuration reads a duration like "15m" or "720h" or falls back to a default.
func getDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// parseSameSite maps a configured name to the cookie SameSite policy.
func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(value) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
