package platform

import (
	"strings"
	"testing"
)

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

// Sharing the domain is allowed, but only when SAID. Resend's free tier verifies
// one domain, so a single-domain deployment is a real position to hold — and it
// is indistinguishable, from the config alone, from a typo that points the
// Digest at the transactional domain by accident. The flag is what tells them
// apart, which is why nothing infers it from the domains matching.

func TestSharingTheTransactionalDomainIsRefusedUntilItIsDeclared(t *testing.T) {
	transactional := "Multiticketing <noreply@send.multiticketing.com>"
	cfg := DigestEmailConfig{
		ResendAPIKey: "re_digest",
		From:         "Multiticketing <digest@send.multiticketing.com>",
	}

	reason := cfg.UnconfiguredReason(transactional)
	if reason == "" {
		t.Fatal("an undeclared shared sending domain was accepted; a typo in DIGEST_EMAIL_FROM would silently send marketing mail from the domain the One-time Passcodes go out on")
	}
	// The refusal has to name the way out, or the operator who meant it has no
	// route from the error to the setting that grants it.
	if !strings.Contains(reason, "DIGEST_EMAIL_ALLOW_SHARED_DOMAIN") {
		t.Fatalf("refusal %q does not name the setting that permits sharing", reason)
	}
}

func TestSharingTheTransactionalDomainIsAllowedOnceDeclared(t *testing.T) {
	transactional := "Multiticketing <noreply@send.multiticketing.com>"
	cfg := DigestEmailConfig{
		ResendAPIKey:             "re_digest",
		From:                     "Multiticketing <digest@send.multiticketing.com>",
		AllowSharedSendingDomain: true,
	}

	if reason := cfg.UnconfiguredReason(transactional); reason != "" {
		t.Fatalf("a declared shared sending domain was still refused: %s", reason)
	}
	// The flag permits the SHARED DOMAIN and nothing else. It must not paper over
	// a genuinely absent identity, which is a different fault with the same cure.
	half := DigestEmailConfig{From: cfg.From, AllowSharedSendingDomain: true}
	if half.UnconfiguredReason(transactional) == "" {
		t.Fatal("the shared-domain flag excused a missing API key; it may only excuse the domain")
	}
}

// One domain means one Resend domain object and one account-scoped key. Making
// the operator store that key twice would not buy isolation — it would buy a
// rotation hazard, where the transactional copy is rotated and the Digest copy
// goes stale until some later Thursday.
func TestASharedDomainDigestFallsBackToTheTransactionalKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("RESEND_API_KEY", "re_transactional")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@send.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", "true")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DigestEmail.ResendAPIKey != "re_transactional" {
		t.Fatalf("digest key = %q, want the transactional key borrowed for the shared domain", cfg.DigestEmail.ResendAPIKey)
	}
	if !cfg.DigestEmail.Configured() {
		t.Fatal("a shared-domain digest with a borrowed key reads as unconfigured; it would send nothing")
	}
	if reason := cfg.DigestEmail.UnconfiguredReason(cfg.EmailFrom); reason != "" {
		t.Fatalf("a fully declared shared-domain digest was refused: %s", reason)
	}
}

// The fallback is the one place the Digest may read the transactional identity,
// so its gate is the test that matters most: without the flag, an unset key must
// still mean no Digests, exactly as it did before the flag existed.
func TestTheKeyFallbackIsUnreachableWithoutTheFlag(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("RESEND_API_KEY", "re_transactional")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@digest.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DigestEmail.ResendAPIKey != "" {
		t.Fatalf("digest key = %q with no flag set; the transactional key leaked into the Digest identity", cfg.DigestEmail.ResendAPIKey)
	}
	if cfg.DigestEmail.Configured() {
		t.Fatal("digest reads as configured on a borrowed key it was never granted")
	}
}

func TestAnExplicitDigestKeyIsNeverOverwrittenByTheFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/ticket_pos")
	t.Setenv("RESEND_API_KEY", "re_transactional")
	t.Setenv("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>")
	t.Setenv("DIGEST_RESEND_API_KEY", "re_digest_restricted")
	t.Setenv("DIGEST_EMAIL_FROM", "Multiticketing <digest@send.multiticketing.com>")
	t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", "true")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DigestEmail.ResendAPIKey != "re_digest_restricted" {
		t.Fatalf("digest key = %q, want the explicitly supplied key; a restricted key must survive sharing the domain", cfg.DigestEmail.ResendAPIKey)
	}
}

// The gate opens on "true" and on nothing else. A value nobody can parse is a
// value nobody meant, and this particular gate defaults to the safe side.
func TestOnlyATrueValueDeclaresSharing(t *testing.T) {
	cases := map[string]bool{
		"true": true, "TRUE": true, "1": true, " true ": true,
		"false": false, "": false, "yes": false, "no": false, "tru": false,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN", value)
			if got := envIsTrue("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN"); got != want {
				t.Fatalf("envIsTrue(%q) = %v, want %v", value, got, want)
			}
		})
	}
}
