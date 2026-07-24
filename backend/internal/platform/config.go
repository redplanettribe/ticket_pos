package platform

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// DevStorefrontBaseURL is where the Storefront answers on the local Compose
// stack. It is a fallback for development only: a Confirmation Link built on it
// is useless anywhere else, which is why NewApp says so out loud if production
// is still using it.
const DevStorefrontBaseURL = "http://localhost:64300"

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
	// StorefrontBaseURL is the Storefront's own public origin. The API needs it
	// to build the Confirmation Link carried in every Sale Confirmation: the link
	// points at the Storefront, not at this API, because a Customer must land on a
	// page and no browser may address the API directly (ADR 0008).
	StorefrontBaseURL string
	// ConfirmationLinkSecret is the HMAC key every Confirmation Link is signed
	// with. It is the whole of that credential's security: anyone holding it can
	// mint a link to any Ticket Sale, so it is read from the environment like
	// every other secret and never defaulted in production.
	ConfirmationLinkSecret string
	// OTPGlobalCeiling caps passcode emails sent platform-wide per rate window,
	// across staff and customer sign-in alike. Zero means "use the package
	// default". Tunable without a deploy-time code change because the right
	// number moves with real traffic.
	OTPGlobalCeiling int
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	otpCeiling, err := envIntOrZero("OTP_GLOBAL_CEILING")
	if err != nil {
		return Config{}, err
	}

	appEnv := envOrDefault("APP_ENV", "development")

	// A Confirmation Link is a bearer credential for a Ticket Sale, so production
	// must never sign one with a fallback key. That requirement is enforced in
	// server.NewApp, at the point the key is actually used, and not here: this
	// same LoadConfig runs in cmd/migrate, whose Cloud Run Job identity reads
	// only the connection string and has no business holding a signing key.
	// Requiring it here failed the migrate Job on every production deploy.
	confirmationLinkSecret := strings.TrimSpace(os.Getenv("CONFIRMATION_LINK_SECRET"))

	cfg := Config{
		AppEnv:        appEnv,
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

		// The dev-stack Storefront origin. Production injects the real one, as it
		// already does for the Storefront container itself; until a custom domain
		// is mapped it has none to inject, which NewApp warns about at startup.
		StorefrontBaseURL:      strings.TrimRight(envOrDefault("STOREFRONT_BASE_URL", DevStorefrontBaseURL), "/"),
		ConfirmationLinkSecret: confirmationLinkSecret,

		OTPGlobalCeiling: otpCeiling,
	}
	return cfg, nil
}

// envIntOrZero reads an optional positive integer setting. An unset variable
// means zero ("use the default"); a malformed one is a startup error rather
// than a silent fallback, because misreading an abuse limit as "unset" is
// exactly the mistake that stays invisible until it matters.
func envIntOrZero(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, raw)
	}
	return value, nil
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
