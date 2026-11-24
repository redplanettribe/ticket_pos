package platform

import (
	"strings"
	"testing"
)

// The SMTP sender is the local stack's Mailpit and nothing else's (#872). It
// is selected only by EMAIL_PROVIDER=smtp or DIGEST_EMAIL_PROVIDER=smtp, both
// refused in production, and its two settings are a pair: half of them fails
// startup rather than leaving a developer reading the log for mail that was
// meant to be in Mailpit.

// smtpEnv is the local stack's configuration: both identities on smtp, each
// with its From, and Mailpit's host and port.
func smtpEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", "development")
	t.Setenv("EMAIL_PROVIDER", "smtp")
	t.Setenv("DIGEST_EMAIL_PROVIDER", "smtp")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@digest.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "")
	t.Setenv("SMTP_HOST", "mailpit")
	t.Setenv("SMTP_PORT", "1025")
}

func TestSMTPProviderWithBothSettingsLoadsForBothIdentities(t *testing.T) {
	smtpEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.EmailProvider != EmailProviderSMTP || cfg.DigestEmail.Provider != EmailProviderSMTP {
		t.Fatalf("providers = %q / %q, want smtp for both", cfg.EmailProvider, cfg.DigestEmail.Provider)
	}
	if cfg.SMTP != (SMTPConfig{Host: "mailpit", Port: 1025}) || cfg.SMTP.Addr() != "mailpit:1025" {
		t.Fatalf("SMTP = %+v (%s), want mailpit:1025", cfg.SMTP, cfg.SMTP.Addr())
	}
	// A Digest on smtp is its From alone, as on SES: no Resend key is read
	// or required.
	if !cfg.DigestEmail.Configured() || cfg.DigestEmail.PartiallyConfigured() {
		t.Fatalf("the Digest on smtp with a From is not configured: %+v", cfg.DigestEmail)
	}
	if reason := cfg.DigestEmail.UnconfiguredReason(cfg.EmailFrom); reason != "" {
		t.Fatalf("UnconfiguredReason = %q, want none", reason)
	}
}

func TestADigestOnSMTPWithoutAFromIsUnconfigured(t *testing.T) {
	smtpEnv(t)
	t.Setenv("DIGEST_EMAIL_FROM", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	reason := cfg.DigestEmail.UnconfiguredReason(cfg.EmailFrom)
	if reason != `DIGEST_EMAIL_PROVIDER is "smtp" but DIGEST_EMAIL_FROM is not set` {
		t.Fatalf("UnconfiguredReason = %q", reason)
	}
}

// Production never sends through the SMTP sender, on either identity, even
// with every SMTP setting present.
func TestSMTPProviderIsRefusedInProduction(t *testing.T) {
	for _, variable := range []string{"EMAIL_PROVIDER", "DIGEST_EMAIL_PROVIDER"} {
		t.Run(variable, func(t *testing.T) {
			smtpEnv(t)
			t.Setenv("APP_ENV", "production")
			t.Setenv("EMAIL_PROVIDER", "")
			t.Setenv("DIGEST_EMAIL_PROVIDER", "")
			t.Setenv(variable, "smtp")

			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("%s=smtp was accepted in production", variable)
			}
			if msg := err.Error(); !strings.HasPrefix(msg, variable+` is "smtp"`) || !strings.Contains(msg, "never used in production") {
				t.Fatalf("error %q does not name %s and the production refusal", msg, variable)
			}
		})
	}
}

// A half-set pair fails startup naming what is missing, whether or not an
// identity selected smtp: with smtp selected the mail would have nowhere to
// go, and without it the half-written setting is a typo for the one above.
func TestHalfSetSMTPSettingsFailStartup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		host     string
		port     string
		want     string
	}{
		{"smtp selected, nothing set", "smtp", "", "", `EMAIL_PROVIDER is "smtp" but SMTP_HOST, SMTP_PORT are not set`},
		{"smtp selected, host only", "smtp", "mailpit", "", "SMTP_HOST is set but SMTP_PORT is not"},
		{"smtp selected, port only", "smtp", "", "1025", "SMTP_PORT is set but SMTP_HOST is not"},
		{"smtp not selected, host only", "", "mailpit", "", "SMTP_HOST is set but SMTP_PORT is not"},
		{"smtp not selected, port only", "", "", "1025", "SMTP_PORT is set but SMTP_HOST is not"},
		{"smtp selected, port not a number", "smtp", "mailpit", "mailpit", `SMTP_PORT must be a port number from 1 to 65535, got "mailpit"`},
		{"smtp selected, port out of range", "smtp", "mailpit", "70000", `SMTP_PORT must be a port number from 1 to 65535, got "70000"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smtpEnv(t)
			t.Setenv("EMAIL_PROVIDER", tc.provider)
			t.Setenv("DIGEST_EMAIL_PROVIDER", "")
			t.Setenv("SMTP_HOST", tc.host)
			t.Setenv("SMTP_PORT", tc.port)

			_, err := LoadConfig()
			if err == nil {
				t.Fatal("a half-set SMTP configuration was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q, want it to say %q", err, tc.want)
			}
		})
	}
}

// Both settings present with neither identity on smtp change nothing: it is a
// developer who switched a provider back, as unused SES settings are.
func TestSMTPSettingsAreIgnoredWhenNoIdentitySelectsSMTP(t *testing.T) {
	smtpEnv(t)
	t.Setenv("EMAIL_PROVIDER", "")
	t.Setenv("DIGEST_EMAIL_PROVIDER", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.SMTP != (SMTPConfig{}) {
		t.Fatalf("SMTP = %+v, want the zero value when no identity selects smtp", cfg.SMTP)
	}
}
