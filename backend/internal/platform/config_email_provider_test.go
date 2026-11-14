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

// Unset reads as Resend, exactly as before the switch existed. Values are
// matched case-insensitively and trimmed: the switch names a provider, and
// " Resend " names the same one as "resend".
func TestUnsetOrNamedResendProviderReadsAsResend(t *testing.T) {
	for _, value := range []string{"", "resend", "RESEND", " Resend ", "resend\n"} {
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
// that most needs to be loud. The Follow Digest refuses "ses" too until the SES
// sender can send a Digest.
func TestAnUnknownEmailProviderFailsStartup(t *testing.T) {
	cases := []struct {
		variable string
		values   []string
		accepted []string
	}{
		{"EMAIL_PROVIDER", []string{"sendgrid", "resend2", "re send", "amazon"}, []string{`"resend"`, `"ses"`}},
		{"DIGEST_EMAIL_PROVIDER", []string{"ses", "sendgrid", "resend2", "re send"}, []string{`"resend"`}},
	}
	for _, tc := range cases {
		for _, value := range tc.values {
			t.Run(tc.variable+"="+value, func(t *testing.T) {
				t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
				t.Setenv("EMAIL_PROVIDER", "")
				t.Setenv("DIGEST_EMAIL_PROVIDER", "")
				t.Setenv(tc.variable, value)

				_, err := LoadConfig()
				if err == nil {
					t.Fatalf("%s=%q was accepted", tc.variable, value)
				}
				// The message names the variable and the values it accepts, so the
				// operator can go from the error straight to the fix.
				msg := err.Error()
				if !strings.HasPrefix(msg, tc.variable+" ") {
					t.Fatalf("error %q does not lead with %s", msg, tc.variable)
				}
				for _, accepted := range tc.accepted {
					if !strings.Contains(msg, accepted) {
						t.Fatalf("error %q does not name the accepted value %s", msg, accepted)
					}
				}
			})
		}
	}
}

// sesEnv is a deployment that selects SES for the transactional identity with
// every SES setting present.
func sesEnv(t *testing.T, appEnv string) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("APP_ENV", appEnv)
	t.Setenv("DIGEST_EMAIL_PROVIDER", "")
	t.Setenv("EMAIL_PROVIDER", "ses")
	t.Setenv("SES_REGION", "us-east-1")
	t.Setenv("SES_CONFIGURATION_SET", "ticket-pos-transactional")
	t.Setenv("AWS_ROLE_ARN", sesTestRoleARN)
	t.Setenv("SES_WEB_IDENTITY_AUDIENCE", sesTestAudience)
}

const (
	sesTestRoleARN  = "arn:aws:iam::123456789012:role/ticket-pos-api-ses"
	sesTestAudience = "sts.amazonaws.com/ticket-pos-api"
)

// EMAIL_PROVIDER=ses with complete settings selects SES and carries them, in
// the same case-insensitive way "resend" is read.
func TestSESProviderWithCompleteSettingsLoads(t *testing.T) {
	for _, value := range []string{"ses", "SES", " Ses "} {
		t.Run(value, func(t *testing.T) {
			sesEnv(t, "production")
			t.Setenv("EMAIL_PROVIDER", value)

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.EmailProvider != EmailProviderSES {
				t.Fatalf("EmailProvider = %q, want %q", cfg.EmailProvider, EmailProviderSES)
			}
			want := SESConfig{
				Region:              "us-east-1",
				ConfigurationSet:    "ticket-pos-transactional",
				RoleARN:             sesTestRoleARN,
				WebIdentityAudience: sesTestAudience,
			}
			if cfg.SES != want {
				t.Fatalf("SES = %+v, want %+v", cfg.SES, want)
			}
			if cfg.DigestEmail.Provider != EmailProviderResend {
				t.Fatalf("DigestEmail.Provider = %q, want resend: the identities switch independently", cfg.DigestEmail.Provider)
			}
		})
	}
}

// Selecting SES with a setting missing fails startup naming what is missing,
// in production and everywhere else alike: an identity that selected SES must
// never quietly send through another provider instead.
func TestSESProviderWithAMissingSettingFailsStartup(t *testing.T) {
	cases := []struct {
		name    string
		unset   []string
		missing []string
	}{
		{"no region", []string{"SES_REGION"}, []string{"SES_REGION"}},
		{"no configuration set", []string{"SES_CONFIGURATION_SET"}, []string{"SES_CONFIGURATION_SET"}},
		{"nothing", []string{"SES_REGION", "SES_CONFIGURATION_SET"}, []string{"SES_REGION", "SES_CONFIGURATION_SET"}},
	}
	for _, appEnv := range []string{"production", "development"} {
		for _, tc := range cases {
			t.Run(appEnv+"/"+tc.name, func(t *testing.T) {
				sesEnv(t, appEnv)
				for _, variable := range tc.unset {
					t.Setenv(variable, " ")
				}

				_, err := LoadConfig()
				if err == nil {
					t.Fatal("LoadConfig accepted EMAIL_PROVIDER=ses with a missing SES setting")
				}
				msg := err.Error()
				if !strings.HasPrefix(msg, "EMAIL_PROVIDER ") {
					t.Fatalf("error %q does not lead with the switch that asked for SES", msg)
				}
				for _, variable := range tc.missing {
					if !strings.Contains(msg, variable) {
						t.Fatalf("error %q does not name the missing %s", msg, variable)
					}
				}
			})
		}
	}
}

// Production authenticates to AWS through Workload Identity Federation only
// (ADR 0077): selecting SES without the role to assume or the audience its ID
// token is minted for fails startup naming what is missing, so a Cloud Run
// service never falls back to some other AWS credential.
func TestSESProviderInProductionWithoutFederationFailsStartup(t *testing.T) {
	cases := []struct {
		name    string
		unset   []string
		missing []string
	}{
		{"no role", []string{"AWS_ROLE_ARN"}, []string{"AWS_ROLE_ARN"}},
		{"no audience", []string{"SES_WEB_IDENTITY_AUDIENCE"}, []string{"SES_WEB_IDENTITY_AUDIENCE"}},
		{"neither", []string{"AWS_ROLE_ARN", "SES_WEB_IDENTITY_AUDIENCE"}, []string{"AWS_ROLE_ARN", "SES_WEB_IDENTITY_AUDIENCE"}},
		{"nothing at all", []string{"SES_REGION", "SES_CONFIGURATION_SET", "AWS_ROLE_ARN", "SES_WEB_IDENTITY_AUDIENCE"},
			[]string{"SES_REGION", "SES_CONFIGURATION_SET", "AWS_ROLE_ARN", "SES_WEB_IDENTITY_AUDIENCE"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sesEnv(t, "production")
			for _, variable := range tc.unset {
				t.Setenv(variable, " ")
			}

			_, err := LoadConfig()
			if err == nil {
				t.Fatal("LoadConfig accepted EMAIL_PROVIDER=ses in production without federation settings")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "EMAIL_PROVIDER ") {
				t.Fatalf("error %q does not lead with the switch that asked for SES", msg)
			}
			for _, variable := range tc.missing {
				if !strings.Contains(msg, variable) {
					t.Fatalf("error %q does not name the missing %s", msg, variable)
				}
			}
		})
	}
}

// Outside production no role is needed: the sender then uses the SDK's
// default credential chain, which is what a developer running the send check
// relies on. A role set without an audience is still refused, because the
// exchange it asks for cannot be made.
func TestSESProviderOutsideProductionMayLeaveTheRoleUnset(t *testing.T) {
	t.Run("no role, no audience loads", func(t *testing.T) {
		sesEnv(t, "development")
		t.Setenv("AWS_ROLE_ARN", "")
		t.Setenv("SES_WEB_IDENTITY_AUDIENCE", "")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.SES.RoleARN != "" {
			t.Fatalf("RoleARN = %q, want empty: the default chain is used", cfg.SES.RoleARN)
		}
	})
	t.Run("a role without an audience fails", func(t *testing.T) {
		sesEnv(t, "development")
		t.Setenv("SES_WEB_IDENTITY_AUDIENCE", "")

		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "SES_WEB_IDENTITY_AUDIENCE") {
			t.Fatalf("err = %v, want a refusal naming SES_WEB_IDENTITY_AUDIENCE", err)
		}
	})
}

// SES settings are neither read nor validated unless an identity selects SES:
// half-written AWS settings on a Resend deployment change nothing.
func TestSESSettingsAreIgnoredWhenNoIdentitySelectsSES(t *testing.T) {
	t.Run("partial settings do not fail startup", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
		t.Setenv("APP_ENV", "production")
		t.Setenv("EMAIL_PROVIDER", "")
		t.Setenv("DIGEST_EMAIL_PROVIDER", "")
		t.Setenv("SES_REGION", "us-east-1")
		t.Setenv("SES_CONFIGURATION_SET", "")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.SES != (SESConfig{}) {
			t.Fatalf("SES = %+v, want the zero value when nothing selects SES", cfg.SES)
		}
	})
	t.Run("complete settings are not read", func(t *testing.T) {
		sesEnv(t, "production")
		t.Setenv("EMAIL_PROVIDER", "resend")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.SES != (SESConfig{}) {
			t.Fatalf("SES = %+v, want the zero value when nothing selects SES", cfg.SES)
		}
	})
}
