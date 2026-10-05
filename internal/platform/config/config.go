package config

import (
	"os"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
) 

type Config struct {
	App      AppConfig
	CORS     CORSConfig
	Mongo    MongoConfig
	Redis    RedisConfig
	JWT      JWTConfig
	Auth     AuthConfig
	Log      LogConfig
	Paystack PaystackConfig
	ImageKit ImageKitConfig
	Resend   ResendConfig
}

type AppConfig struct {
	Env     string
	Name    string
	Port    int
	BaseURL string
}

type CORSConfig struct {
	WebOrigin   string
	AdminOrigin string
}

type MongoConfig struct {
	URI      string
	Database string
}

type RedisConfig struct {
	URL string
}

type JWTConfig struct {
	AccessSecret string
	AccessTTL    time.Duration
}

type AuthConfig struct {
	RefreshTTL time.Duration
	BcryptCost int
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
}

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

	cfg := Config{
		App: AppConfig{
			Env:     getString("APP_ENV", "development"),
			Name:    getString("APP_NAME", "denisco-api"),
			Port:    getInt("API_PORT", 4000),
			BaseURL: getString("API_BASE_URL", "http://localhost:4000"),
		},
		CORS: CORSConfig{
			WebOrigin:   required("WEB_ORIGIN"),
			AdminOrigin: required("ADMIN_ORIGIN"),
		},
		Mongo: MongoConfig{
			URI:      required("MONGODB_URI"),
			Database: getString("MONGODB_DATABASE", "denisco"),
		},
		Redis: RedisConfig{
			URL: getString("REDIS_URL", "redis://localhost:6379/0"),
		},
		JWT: JWTConfig{
			AccessSecret: required("JWT_ACCESS_SECRET"),
			AccessTTL:    getDuration("JWT_ACCESS_TTL", 15*time.Minute),
		},
		Auth: AuthConfig{
			RefreshTTL: getDuration("REFRESH_TOKEN_TTL", 720*time.Hour),
			BcryptCost: getInt("BCRYPT_COST", 12),
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
			APIKey:    getString("RESEND_API_KEY", ""),
			EmailFrom: getString("EMAIL_FROM", ""),
		},
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
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