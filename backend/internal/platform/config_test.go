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

// PAYPHONE_API_BASE_URL decides which server the platform believes collected a
// Customer's money. Same shape as the Google token endpoint above, same one
// rule: it may move outside production and nowhere else, and the process
// refuses to come up rather than serve with it set.

func TestPayPhoneBaseURLDefaultsToPayPhone(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "production")
	t.Setenv("PAYPHONE_API_BASE_URL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.PayPhone.BaseURL != PayPhoneBaseURL {
		t.Fatalf("payphone base url = %q, want %q", cfg.PayPhone.BaseURL, PayPhoneBaseURL)
	}
}

func TestPayPhoneBaseURLOverrideFailsStartupInProduction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "production")
	t.Setenv("PAYPHONE_API_BASE_URL", "https://payments.example.com")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig succeeded with PAYPHONE_API_BASE_URL set in production; it must refuse to start")
	}
	if !strings.Contains(err.Error(), "PAYPHONE_API_BASE_URL") {
		t.Fatalf("error = %v, want it to name PAYPHONE_API_BASE_URL so the operator can see what to remove", err)
	}
}

func TestPayPhoneBaseURLOverrideIsAllowedOutsideProduction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("PAYPHONE_API_BASE_URL", "http://127.0.0.1:1234")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.PayPhone.BaseURL != "http://127.0.0.1:1234" {
		t.Fatalf("payphone base url = %q, want the override — the integration suite's fake server depends on it", cfg.PayPhone.BaseURL)
	}
}

// The credentials select the provider (ADR 0012): both present means PayPhone,
// anything less means the stub. They are ordinary secrets and not required — a
// deployment without them starts and sells through the stub.
func TestPayPhoneCredentialsSelectTheProvider(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("PAYPHONE_API_TOKEN", "payphone-token")
	t.Setenv("PAYPHONE_STORE_ID", "store-123")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.PayPhone.Configured() {
		t.Fatalf("payphone = %+v, want Configured with both credentials set", cfg.PayPhone)
	}

	// Half a credential pair is not a configuration; it must select the stub
	// rather than a PayPhone client that cannot authenticate.
	t.Setenv("PAYPHONE_STORE_ID", "")
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig without a store id: %v — the platform must start without it", err)
	}
	if cfg.PayPhone.Configured() {
		t.Fatalf("payphone = %+v, want not Configured with only a token", cfg.PayPhone)
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

// The Platform Fee schedule is an ops knob (ADR 0014): the next IVA change
// should be an environment edit, and a fat-fingered one should stop the process
// rather than quietly bill at the wrong rate.

func TestFeeRatesDefaultToTheLaunchSchedule(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Fees.FeeBasisPoints != DefaultPlatformFeeBasisPoints || cfg.Fees.FeeIVABasisPoints != DefaultPlatformFeeIVABasisPoints {
		t.Fatalf("fee schedule = %+v, want the launch %d/%d bps", cfg.Fees, DefaultPlatformFeeBasisPoints, DefaultPlatformFeeIVABasisPoints)
	}
}

func TestFeeRatesAreConfigurable(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("PLATFORM_FEE_BASIS_POINTS", "0")
	t.Setenv("PLATFORM_FEE_IVA_BASIS_POINTS", "1200")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// Zero is a rate, not "unset": a deployment can switch the fee off.
	if cfg.Fees.FeeBasisPoints != 0 || cfg.Fees.FeeIVABasisPoints != 1200 {
		t.Fatalf("fee schedule = %+v, want 0/1200 bps", cfg.Fees)
	}
}

func TestOutOfRangeFeeRateFailsStartup(t *testing.T) {
	for _, raw := range []string{"ten percent", "-1", "10001"} {
		t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
		t.Setenv("PLATFORM_FEE_BASIS_POINTS", raw)

		_, err := LoadConfig()
		if err == nil {
			t.Fatalf("LoadConfig succeeded with PLATFORM_FEE_BASIS_POINTS=%q; it must refuse to start", raw)
		}
		if !strings.Contains(err.Error(), "PLATFORM_FEE_BASIS_POINTS") {
			t.Fatalf("error = %v, want it to name PLATFORM_FEE_BASIS_POINTS", err)
		}
	}
}

// INVOICING_CERTIFICATE_KEY is the key the Issuer's signing certificate is
// kept encrypted under (#453, ADR 0059). Unset is a running state; set but not
// 32 bytes is a startup failure.

func TestInvoicingCertificateKeyIsOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "production")
	t.Setenv("INVOICING_CERTIFICATE_KEY", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.InvoicingCertificateKey != nil {
		t.Fatalf("key = %v, want nil when unset", cfg.InvoicingCertificateKey)
	}
}

func TestInvoicingCertificateKeyDecodesTo32Bytes(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	// 32 bytes of 0x42, standard base64 with padding.
	t.Setenv("INVOICING_CERTIFICATE_KEY", " QkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkI= ")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.InvoicingCertificateKey) != 32 {
		t.Fatalf("key length = %d, want 32", len(cfg.InvoicingCertificateKey))
	}
	for i, b := range cfg.InvoicingCertificateKey {
		if b != 0x42 {
			t.Fatalf("key[%d] = %#x, want 0x42", i, b)
		}
	}
}

func TestInvoicingCertificateKeyRefusesOtherLengthsAndBadBase64(t *testing.T) {
	cases := map[string]string{
		"16 bytes":   "QUFBQUFBQUFBQUFBQUFBQQ==",
		"31 bytes":   "QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQQ==",
		"33 bytes":   "QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB",
		"not base64": "not*base64*at*all",
		"raw hex":    "4242424242424242424242424242424242424242424242424242424242424242",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
			t.Setenv("INVOICING_CERTIFICATE_KEY", value)
			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "INVOICING_CERTIFICATE_KEY") {
				t.Fatalf("LoadConfig error = %v, want a refusal naming INVOICING_CERTIFICATE_KEY", err)
			}
		})
	}
}
