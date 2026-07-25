package platform

import (
	"strings"
	"testing"
)

// GOOGLE_TOKEN_ENDPOINT decides which issuer the platform will believe about who
// somebody is. Everything below is about the one rule that makes it safe to
// exist at all: it may move outside production and nowhere else.
//
// This is unit-tested rather than covered at the HTTP seam because it is a
// property of startup, not of a request: the point is that the process refuses
// to come up.

func TestGoogleTokenEndpointDefaultsToGoogle(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "production")
	t.Setenv("GOOGLE_TOKEN_ENDPOINT", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Google.TokenEndpoint != GoogleTokenEndpoint {
		t.Fatalf("token endpoint = %q, want %q", cfg.Google.TokenEndpoint, GoogleTokenEndpoint)
	}
}

func TestGoogleTokenEndpointOverrideFailsStartupInProduction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "production")
	t.Setenv("GOOGLE_TOKEN_ENDPOINT", "https://issuer.example.com/token")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig succeeded with GOOGLE_TOKEN_ENDPOINT set in production; it must refuse to start")
	}
	if !strings.Contains(err.Error(), "GOOGLE_TOKEN_ENDPOINT") {
		t.Fatalf("error = %v, want it to name GOOGLE_TOKEN_ENDPOINT so the operator can see what to remove", err)
	}
}

func TestGoogleTokenEndpointOverrideIsAllowedOutsideProduction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("GOOGLE_TOKEN_ENDPOINT", "http://127.0.0.1:1234/token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Google.TokenEndpoint != "http://127.0.0.1:1234/token" {
		t.Fatalf("token endpoint = %q, want the override — the integration suite's stub depends on it", cfg.Google.TokenEndpoint)
	}
}

// The two surfaces' clients are read from their own variables and never fall
// back to one another. Loading the Storefront's secret into the Staff client
// would silently dissolve the isolation ADR 0011 rests on, and it would look
// like everything working.
func TestGoogleClientsAreReadPerSurface(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("GOOGLE_STAFF_CLIENT_ID", "staff-client-id")
	t.Setenv("GOOGLE_STAFF_CLIENT_SECRET", "staff-client-secret")
	t.Setenv("GOOGLE_STOREFRONT_CLIENT_ID", "storefront-client-id")
	t.Setenv("GOOGLE_STOREFRONT_CLIENT_SECRET", "storefront-client-secret")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Google.Staff.ClientID != "staff-client-id" || cfg.Google.Staff.ClientSecret != "staff-client-secret" {
		t.Fatalf("staff client = %+v, want the configured values", cfg.Google.Staff)
	}
	if cfg.Google.Storefront.ClientID != "storefront-client-id" || cfg.Google.Storefront.ClientSecret != "storefront-client-secret" {
		t.Fatalf("storefront client = %+v, want the configured values", cfg.Google.Storefront)
	}

	// One surface configured and the other not is an ordinary state — the
	// Storefront shipped first — and it must not lend the configured client to
	// the unconfigured surface.
	t.Setenv("GOOGLE_STAFF_CLIENT_ID", "")
	t.Setenv("GOOGLE_STAFF_CLIENT_SECRET", "")
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with no staff credentials: %v — the platform must start without them", err)
	}
	if cfg.Google.Staff.ClientID != "" || cfg.Google.Staff.ClientSecret != "" {
		t.Fatalf("staff client = %+v, want empty — it must not inherit the Storefront's", cfg.Google.Staff)
	}
}

// The client credentials are ordinary secrets and are not required: a
// deployment without them serves every other path, and Google Sign-In refuses.
func TestGoogleStorefrontCredentialsAreOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("GOOGLE_STOREFRONT_CLIENT_ID", "storefront-client-id")
	t.Setenv("GOOGLE_STOREFRONT_CLIENT_SECRET", "storefront-client-secret")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Google.Storefront.ClientID != "storefront-client-id" || cfg.Google.Storefront.ClientSecret != "storefront-client-secret" {
		t.Fatalf("storefront client = %+v, want the configured values", cfg.Google.Storefront)
	}

	t.Setenv("GOOGLE_STOREFRONT_CLIENT_ID", "")
	t.Setenv("GOOGLE_STOREFRONT_CLIENT_SECRET", "")
	if _, err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig without Google credentials: %v — the platform must start without them", err)
	}
}
