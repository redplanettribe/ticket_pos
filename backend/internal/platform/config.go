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

// GoogleTokenEndpoint is Google's own token endpoint, where every authorization
// code is redeemed unless a non-production deployment says otherwise.
const GoogleTokenEndpoint = "https://oauth2.googleapis.com/token"

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
	// Google holds what the API needs to complete a Google Sign-In on either
	// surface. See GoogleConfig.
	Google GoogleConfig
	// PayPhone holds the platform's single PayPhone merchant credentials
	// (ADR 0012). Their presence selects the PayPhone Payment Provider; absent,
	// the stub provider serves so checkout works locally with zero setup.
	PayPhone PayPhoneConfig
}

// PayPhoneConfig is the platform's registration with PayPhone: one merchant
// account for the whole platform, settled with Organizations off-system
// (ADR 0012). Both values are secrets in production, injected like every other
// secret; neither is required — a deployment without them runs on the stub
// Payment Provider.
type PayPhoneConfig struct {
	APIToken string
	StoreID  string
}

// Configured reports whether PayPhone credentials are present — the switch that
// selects the real Payment Provider over the stub.
func (c PayPhoneConfig) Configured() bool {
	return c.APIToken != "" && c.StoreID != ""
}

// GoogleOAuthClient is one surface's registration with Google.
//
// Each surface has its own (ADR 0011), because Google binds an authorization
// code to the client that requested it: a code obtained on the Storefront
// cannot be redeemed with the Staff client's credentials, so cross-surface
// isolation is a property of these values rather than of a check in application
// code. A leaked Storefront secret therefore reaches no Organization.
type GoogleOAuthClient struct {
	ClientID     string
	ClientSecret string
}

// GoogleConfig is the Google Sign-In configuration, one entry per surface plus
// the endpoint they share.
//
// The two clients are siblings and stay siblings: neither is a default for the
// other and there is no "the Google client". A surface that cannot name which
// one it means has a bug, because naming the wrong one is exactly the mistake
// ADR 0011's isolation exists to make impossible.
type GoogleConfig struct {
	// Storefront is the client the Customer surface signs in with.
	Storefront GoogleOAuthClient
	// Staff is the client the Staff surface signs in with. A code obtained on
	// the Storefront is not redeemable with these credentials, which is where
	// cross-surface isolation lives.
	Staff GoogleOAuthClient
	// TokenEndpoint is where authorization codes are redeemed. It defaults to
	// Google's own endpoint and exists to be overridden by exactly one caller:
	// the integration suite, which points it at a stub. LoadConfig refuses an
	// override in production — see loadGoogleConfig.
	TokenEndpoint string
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

	google, err := loadGoogleConfig(appEnv)
	if err != nil {
		return Config{}, err
	}

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
		Google:           google,
		PayPhone: PayPhoneConfig{
			APIToken: strings.TrimSpace(os.Getenv("PAYPHONE_API_TOKEN")),
			StoreID:  strings.TrimSpace(os.Getenv("PAYPHONE_STORE_ID")),
		},
	}
	return cfg, nil
}

// loadGoogleConfig reads the Google Sign-In settings and enforces the one guard
// this feature ships with.
//
// GOOGLE_TOKEN_ENDPOINT decides which issuer the platform trusts to say who
// somebody is. Anyone able to set it could point the exchange at a server they
// control and mint a session for any address on either surface, which is
// unrestricted account takeover — and it would leave no trace, because a
// sign-in through a hostile issuer looks exactly like a real one. It exists for
// the integration suite's stub token endpoint and for nothing else, so in
// production it is not warned about like the Storefront origin fallback, but
// refused: the process does not start.
//
// The check lives here rather than in server.NewApp because it is a refusal
// rather than a requirement. Nothing is asked of a deployment that has not set
// it, so cmd/migrate — which shares this loader and holds none of the API's
// secrets — is unaffected, and every workload built from this config is covered.
//
// The client credentials are not required. A deployment without them starts and
// serves every other path; Google Sign-In then refuses, which is what local
// development without Google credentials looks like.
func loadGoogleConfig(appEnv string) (GoogleConfig, error) {
	tokenEndpoint := strings.TrimSpace(os.Getenv("GOOGLE_TOKEN_ENDPOINT"))
	if tokenEndpoint != "" && appEnv == "production" {
		return GoogleConfig{}, fmt.Errorf("GOOGLE_TOKEN_ENDPOINT must not be set when APP_ENV is production: it decides which issuer the platform trusts and exists only for tests")
	}
	if tokenEndpoint == "" {
		tokenEndpoint = GoogleTokenEndpoint
	}

	return GoogleConfig{
		Storefront: GoogleOAuthClient{
			ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_STOREFRONT_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_STOREFRONT_CLIENT_SECRET")),
		},
		Staff: GoogleOAuthClient{
			ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_STAFF_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_STAFF_CLIENT_SECRET")),
		},
		TokenEndpoint: tokenEndpoint,
	}, nil
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
