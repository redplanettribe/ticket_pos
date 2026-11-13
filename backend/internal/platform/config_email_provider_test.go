package platform

import (
	"strings"
	"testing"
)

// Each sending identity's provider is chosen by an explicit setting, never by
// which credentials happen to be present (ADR 0077). EMAIL_PROVIDER selects the
// transactional identity's provider and DIGEST_EMAIL_PROVIDER the Follow
// Digest's. Resend is the only provider so far and the default, so an unset
// switch must read exactly as it did before the switch existed.

func TestEmailProvidersDefaultToResendWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("EMAIL_PROVIDER", "")
	t.Setenv("DIGEST_EMAIL_PROVIDER", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.EmailProvider != EmailProviderResend {
		t.Fatalf("EmailProvider = %q, want %q", cfg.EmailProvider, EmailProviderResend)
	}
	if cfg.DigestEmail.Provider != EmailProviderResend {
		t.Fatalf("DigestEmail.Provider = %q, want %q", cfg.DigestEmail.Provider, EmailProviderResend)
	}
}

// Values are matched case-insensitively and trimmed: the switch names a
// provider, and " Resend " names the same one as "resend".
func TestAnExplicitResendProviderReadsAsResend(t *testing.T) {
	for _, value := range []string{"resend", "RESEND", " Resend ", "resend\n"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
			t.Setenv("EMAIL_PROVIDER", value)
			t.Setenv("DIGEST_EMAIL_PROVIDER", value)

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.EmailProvider != EmailProviderResend {
				t.Fatalf("EmailProvider = %q, want %q", cfg.EmailProvider, EmailProviderResend)
			}
			if cfg.DigestEmail.Provider != EmailProviderResend {
				t.Fatalf("DigestEmail.Provider = %q, want %q", cfg.DigestEmail.Provider, EmailProviderResend)
			}
		})
	}
}

// Anything else fails startup. A provider nobody can name is a provider nobody
// meant, and falling back to the default would hide exactly the switch-day typo
// that most needs to be loud. "ses" is refused too until the SES sender exists.
func TestAnUnknownEmailProviderFailsStartup(t *testing.T) {
	for _, variable := range []string{"EMAIL_PROVIDER", "DIGEST_EMAIL_PROVIDER"} {
		for _, value := range []string{"ses", "sendgrid", "resend2", "re send"} {
			t.Run(variable+"="+value, func(t *testing.T) {
				t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
				t.Setenv("EMAIL_PROVIDER", "")
				t.Setenv("DIGEST_EMAIL_PROVIDER", "")
				t.Setenv(variable, value)

				_, err := LoadConfig()
				if err == nil {
					t.Fatalf("%s=%q was accepted", variable, value)
				}
				// The message names the variable and the values it accepts, so the
				// operator can go from the error straight to the fix.
				msg := err.Error()
				if !strings.HasPrefix(msg, variable+" ") {
					t.Fatalf("error %q does not lead with %s", msg, variable)
				}
				if !strings.Contains(msg, `"resend"`) {
					t.Fatalf("error %q does not name the accepted values", msg)
				}
			})
		}
	}
}
