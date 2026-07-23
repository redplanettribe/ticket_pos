package platform

import (
	"fmt"
	"log/slog"
	"os"
)

// Config holds application configuration loaded from the environment.
type Config struct {
	AppEnv        string
	DatabaseURL   string
	HTTPAddr      string
	RunMigrations bool
	LogLevel      slog.Level
	S3Endpoint    string
	S3PublicURL   string
	S3AccessKey   string
	S3SecretKey   string
	S3Bucket      string
	S3Region      string
	ResendAPIKey  string
	EmailFrom     string
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	cfg := Config{
		AppEnv:        envOrDefault("APP_ENV", "development"),
		DatabaseURL:   databaseURL,
		HTTPAddr:      envOrDefault("HTTP_ADDR", ":8080"),
		RunMigrations: shouldRunMigrations(),
		LogLevel:      parseLogLevel(os.Getenv("LOG_LEVEL")),
		S3Endpoint:    os.Getenv("S3_ENDPOINT"),
		S3PublicURL:   os.Getenv("S3_PUBLIC_URL"),
		S3AccessKey:   os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:   os.Getenv("S3_SECRET_KEY"),
		S3Bucket:      os.Getenv("S3_BUCKET"),
		S3Region:      envOrDefault("S3_REGION", "us-east-1"),
		ResendAPIKey:  os.Getenv("RESEND_API_KEY"),
		EmailFrom:     envOrDefault("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>"),
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func shouldRunMigrations() bool {
	if os.Getenv("RUN_MIGRATIONS") == "false" {
		return false
	}
	if os.Getenv("APP_ENV") == "production" {
		return os.Getenv("RUN_MIGRATIONS") == "true"
	}
	return true
}

func parseLogLevel(raw string) slog.Level {
	switch raw {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
