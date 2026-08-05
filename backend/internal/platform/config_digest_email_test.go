package platform

import "testing"

// The Follow Digest's sending identity is configured SEPARATELY from the
// transactional one (#225, ADR 0030). Everything below is about the one rule
// that makes the split worth having: neither identity may ever stand in for the
// other, so an absent Digest identity has to read as absent rather than as "use
// the one next to it".
//
// This is unit-tested rather than covered at the HTTP seam for the reason the
// Google token endpoint's rule is: it is a property of startup, not of a
// request.

func TestDigestEmailIsUnconfiguredWhenItsVariablesAreUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("RESEND_API_KEY", "re_transactional")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "")
	t.Setenv("DIGEST_EMAIL_FROM", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DigestEmail.Configured() {
		t.Fatal("digest email reads as configured with neither variable set")
	}
	// The absent case must be EMPTY, not the transactional values. A default here
	// is exactly the silent fallback this feature exists to remove.
	if cfg.DigestEmail.From != "" {
		t.Fatalf("DIGEST_EMAIL_FROM defaulted to %q; it must have no default at all", cfg.DigestEmail.From)
	}
	if cfg.DigestEmail.ResendAPIKey != "" {
		t.Fatalf("DIGEST_RESEND_API_KEY defaulted to %q; it must have no default at all", cfg.DigestEmail.ResendAPIKey)
	}
}

func TestDigestEmailIsReadFromItsOwnVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("RESEND_API_KEY", "re_transactional")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "re_digest")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@digest.multiticketing.com>")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.DigestEmail.Configured() {
		t.Fatal("digest email reads as unconfigured with both variables set")
	}
	if cfg.DigestEmail.ResendAPIKey != "re_digest" {
		t.Fatalf("digest key = %q, want the digest key and never the transactional one", cfg.DigestEmail.ResendAPIKey)
	}
	if cfg.DigestEmail.From != "Multiticketing <digest@digest.multiticketing.com>" {
		t.Fatalf("digest from = %q, want the digest identity", cfg.DigestEmail.From)
	}
	// The transactional identity is untouched by any of it.
	if cfg.ResendAPIKey != "re_transactional" || cfg.EmailFrom != "Multiticketing <noreply@send.multiticketing.com>" {
		t.Fatalf("transactional identity moved: key=%q from=%q", cfg.ResendAPIKey, cfg.EmailFrom)
	}
}

// Half a Digest identity is not a Digest identity. A key with no From has
// nothing to send as, and a From with no key cannot authenticate — either way
// the honest reading is "unconfigured", because the alternative is borrowing
// the missing half from transactional mail.
func TestDigestEmailWithOnlyHalfItsSettingsIsNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		key  string
		from string
	}{
		{"key without a from", "re_digest", ""},
		{"from without a key", "", "Multiticketing <digest@digest.multiticketing.com>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
			t.Setenv("DIGEST_RESEND_API_KEY", tc.key)
			t.Setenv("DIGEST_EMAIL_FROM", tc.from)

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.DigestEmail.Configured() {
				t.Fatal("half-configured digest email reads as configured")
			}
			if !cfg.DigestEmail.PartiallyConfigured() {
				t.Fatal("half-configured digest email does not report itself as partial; the operator would never be told which half is missing")
			}
		})
	}
}

// The whole point of the second identity is that it is on a DIFFERENT sending
// domain (ADR 0030). A Digest From on the transactional domain is the coupling
// this feature removes, wearing a different variable name.
func TestDigestEmailKnowsWhenItSharesTheTransactionalDomain(t *testing.T) {
	transactional := "Multiticketing <noreply@send.multiticketing.com>"
	cases := []struct {
		name   string
		from   string
		shared bool
	}{
		{"its own subdomain", "Multiticketing <digest@digest.multiticketing.com>", false},
		{"the transactional subdomain", "Multiticketing <digest@send.multiticketing.com>", true},
		{"the transactional subdomain, cased differently", "Multiticketing <digest@SEND.Multiticketing.com>", true},
		{"a bare address on the transactional subdomain", "digest@send.multiticketing.com", true},
		{"unset", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DigestEmailConfig{ResendAPIKey: "re_digest", From: tc.from}
			if got := cfg.SharesSendingDomainWith(transactional); got != tc.shared {
				t.Fatalf("SharesSendingDomainWith(%q) = %v, want %v", tc.from, got, tc.shared)
			}
		})
	}
}
