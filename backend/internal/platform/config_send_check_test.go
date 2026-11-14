package platform

import (
	"strings"
	"testing"
)

// The SES send check (#697) reads the SES settings and both From addresses
// the API reads, from the same variables, and nothing else: no database, no
// provider switch, no Resend key, no role to assume.

func sendCheckEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "")
	t.Setenv("EMAIL_PROVIDER", "")
	t.Setenv("DIGEST_EMAIL_PROVIDER", "")
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("SES_REGION", "us-east-1")
	t.Setenv("SES_CONFIGURATION_SET", "prod-ticket-pos")
	t.Setenv("AWS_ROLE_ARN", "")
	t.Setenv("SES_WEB_IDENTITY_AUDIENCE", "")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@digest.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", "")
}

// With no database, no provider switch and no Resend key, the send check
// still loads: both Froms and the shared SES settings.
func TestSendCheckConfigReadsTheAPIsSESSettingsAndBothFroms(t *testing.T) {
	sendCheckEnv(t)

	cfg, err := LoadSendCheckConfig()
	if err != nil {
		t.Fatalf("LoadSendCheckConfig: %v", err)
	}
	if cfg.TransactionalFrom != "Multiticketing <noreply@send.multiticketing.com>" {
		t.Fatalf("TransactionalFrom = %q", cfg.TransactionalFrom)
	}
	if cfg.DigestFrom != "Multiticketing <digest@digest.multiticketing.com>" {
		t.Fatalf("DigestFrom = %q", cfg.DigestFrom)
	}
	if cfg.SES.Region != "us-east-1" || cfg.SES.ConfigurationSet != "prod-ticket-pos" {
		t.Fatalf("SES = %+v", cfg.SES)
	}
}

// EMAIL_FROM unset is the API's own default transactional From.
func TestSendCheckConfigDefaultsTheTransactionalFromAsTheAPIDoes(t *testing.T) {
	sendCheckEnv(t)
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")

	cfg, err := LoadSendCheckConfig()
	if err != nil {
		t.Fatalf("LoadSendCheckConfig: %v", err)
	}
	api, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TransactionalFrom != api.EmailFrom {
		t.Fatalf("TransactionalFrom = %q, API EmailFrom = %q", cfg.TransactionalFrom, api.EmailFrom)
	}
}

// The check runs on the operator's own profile through the default chain.
// A role left in the environment (a sourced Cloud Run env file) is never
// used: the federation exchange needs the GCP metadata server, which an
// operator's machine does not have.
func TestSendCheckConfigNeverAssumesARole(t *testing.T) {
	sendCheckEnv(t)
	t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/prod-ticket-pos-api-ses")
	t.Setenv("SES_WEB_IDENTITY_AUDIENCE", "prod-ticket-pos-api-ses")

	cfg, err := LoadSendCheckConfig()
	if err != nil {
		t.Fatalf("LoadSendCheckConfig: %v", err)
	}
	if cfg.SES.RoleARN != "" || cfg.SES.WebIdentityAudience != "" {
		t.Fatalf("SES = %+v, want no role and no audience", cfg.SES)
	}
}

// Missing SES settings are refused, naming each, rather than sending through
// anything else.
func TestSendCheckConfigRefusesMissingSESSettings(t *testing.T) {
	sendCheckEnv(t)
	t.Setenv("SES_REGION", "")
	t.Setenv("SES_CONFIGURATION_SET", " ")

	_, err := LoadSendCheckConfig()
	if err == nil {
		t.Fatal("missing SES settings were accepted")
	}
	for _, want := range []string{"SES_REGION", "SES_CONFIGURATION_SET"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %s", err, want)
		}
	}
}

// The Digest identity follows the API's rules on SES: it needs its own From,
// and a From on the transactional domain only when declared.
func TestSendCheckConfigAppliesTheDigestRules(t *testing.T) {
	t.Run("no Digest From", func(t *testing.T) {
		sendCheckEnv(t)
		t.Setenv("DIGEST_EMAIL_FROM", "")
		_, err := LoadSendCheckConfig()
		if err == nil || !strings.Contains(err.Error(), "DIGEST_EMAIL_FROM") {
			t.Fatalf("err = %v, want a refusal naming DIGEST_EMAIL_FROM", err)
		}
	})
	t.Run("undeclared shared domain", func(t *testing.T) {
		sendCheckEnv(t)
		t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@send.multiticketing.com>")
		_, err := LoadSendCheckConfig()
		if err == nil || !strings.Contains(err.Error(), "DIGEST_EMAIL_ALLOW_SHARED_DOMAIN") {
			t.Fatalf("err = %v, want the shared-domain refusal", err)
		}
	})
	t.Run("declared shared domain", func(t *testing.T) {
		sendCheckEnv(t)
		t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@send.multiticketing.com>")
		t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", "true")
		if _, err := LoadSendCheckConfig(); err != nil {
			t.Fatalf("LoadSendCheckConfig: %v", err)
		}
	})
}
