package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/policy"
)

// The public Privacy Policy read (#250, parent #249): the current Policy Version
// served in one language, with the full policy, the Short Notice, and the three
// consent checkbox labels in one payload.
//
// What these tests are really about is the tie between the text and its
// fingerprint. The Storefront page, the checkbox labels the capture surfaces in
// #251 will render, and the SHA-256 recorded on the Policy Version row all have
// to be the same artifact — otherwise the hash proves nothing about what a
// person was shown. The unit test in internal/consent/policy pins the seed
// against the embedded text; these pin what actually comes back over HTTP.

type privacyPolicyPayload struct {
	Version       string `json:"version"`
	EffectiveDate string `json:"effective_date"`
	ContentHash   string `json:"content_hash"`
	Locale        string `json:"locale"`
	ShortNotice   string `json:"short_notice"`
	ConsentLabels struct {
		PolicyAcceptance  string `json:"policy_acceptance"`
		MarketingConsent  string `json:"marketing_consent"`
		NetworkingConsent string `json:"networking_consent"`
	} `json:"consent_labels"`
	BodyMarkdown string `json:"body_markdown"`
}

func getPrivacyPolicy(t *testing.T, env *testEnv, locale string) (*http.Response, envelope, privacyPolicyPayload) {
	t.Helper()

	resp, body := env.get(t, "/api/v1/public/privacy-policy/"+locale, nil)
	var payload privacyPolicyPayload
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body.Data, &payload); err != nil {
			t.Fatalf("decode privacy policy: %v", err)
		}
	}
	return resp, body, payload
}

// The feature, in English: an unauthenticated caller reads the whole current
// edition in one request.
func TestPrivacyPolicyIsPublicAndComplete(t *testing.T) {
	env := setupTest(t)

	resp, body, payload := getPrivacyPolicy(t, env, "en")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error on success: %+v", body.Error)
	}
	if body.RequestID == "" {
		t.Error("no request id on the envelope")
	}

	if payload.Version != "0-placeholder" {
		t.Errorf("version = %q, want the seeded placeholder edition", payload.Version)
	}
	if payload.EffectiveDate != "2026-08-01" {
		t.Errorf("effective_date = %q, want 2026-08-01", payload.EffectiveDate)
	}
	if payload.Locale != "en" {
		t.Errorf("locale = %q, want en", payload.Locale)
	}
	for name, text := range map[string]string{
		"short_notice":                      payload.ShortNotice,
		"body_markdown":                     payload.BodyMarkdown,
		"consent_labels.policy_acceptance":  payload.ConsentLabels.PolicyAcceptance,
		"consent_labels.marketing_consent":  payload.ConsentLabels.MarketingConsent,
		"consent_labels.networking_consent": payload.ConsentLabels.NetworkingConsent,
	} {
		if strings.TrimSpace(text) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	// The two optional boxes have to say what they cover: the marketing one names
	// the weekly Follow Digest it switches (ADR 0034), the networking one names
	// both audiences it authorizes (CONTEXT.md).
	if !strings.Contains(payload.ConsentLabels.MarketingConsent, "Follow Digest") {
		t.Errorf("marketing label does not name the Follow Digest: %q", payload.ConsentLabels.MarketingConsent)
	}
	for _, audience := range []string{"attendees", "organizers"} {
		if !strings.Contains(payload.ConsentLabels.NetworkingConsent, audience) {
			t.Errorf("networking label does not name %q: %q", audience, payload.ConsentLabels.NetworkingConsent)
		}
	}
}

// THE ACCEPTANCE CRITERION, end to end: the fingerprint the API publishes is
// the one the seeded row holds, and both are the hash of the text in the same
// response. A future edit to the policy that forgets the version fails here as
// well as in the unit test — this is the copy that also proves the row reached
// the database.
func TestPrivacyPolicyHashMatchesTheSeededPolicyVersion(t *testing.T) {
	env := setupTest(t)

	_, _, payload := getPrivacyPolicy(t, env, "en")

	if computed := policy.ContentHash(); payload.ContentHash != computed {
		t.Fatalf("served content_hash = %q, recomputed from the served artifacts = %q", payload.ContentHash, computed)
	}

	// Read the row directly: no API publishes the policy_versions table, and the
	// point of this assertion is that the seed — not the binary — carries the
	// fingerprint.
	var seededHash, seededLabel string
	err := env.db.QueryRow(`SELECT label, content_hash FROM policy_versions ORDER BY effective_date DESC, created_at DESC LIMIT 1`).
		Scan(&seededLabel, &seededHash)
	if err != nil {
		t.Fatalf("read seeded policy version: %v", err)
	}
	if seededLabel != payload.Version {
		t.Errorf("served version %q is not the current row %q", payload.Version, seededLabel)
	}
	if seededHash != payload.ContentHash {
		t.Fatalf("seeded content hash %q is not what the endpoint served (%q)", seededHash, payload.ContentHash)
	}
}

// One edition, two languages. The Spanish read is genuinely Spanish, and it
// carries the SAME fingerprint as the English one — because a Policy Version is
// an edition of the policy and not of one translation of it.
func TestPrivacyPolicyIsPublishedInSpanishUnderTheSameVersion(t *testing.T) {
	env := setupTest(t)

	_, _, english := getPrivacyPolicy(t, env, "en")
	resp, body, spanish := getPrivacyPolicy(t, env, "es")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if spanish.Locale != "es" {
		t.Errorf("locale = %q, want es", spanish.Locale)
	}
	if spanish.BodyMarkdown == english.BodyMarkdown {
		t.Error("the Spanish policy is byte-identical to the English one")
	}
	if !strings.Contains(spanish.BodyMarkdown, "Quién trata sus datos") {
		t.Error("the Spanish body does not read as Spanish")
	}
	if spanish.Version != english.Version || spanish.ContentHash != english.ContentHash {
		t.Errorf("the two languages are not one edition: en %s/%s, es %s/%s",
			english.Version, english.ContentHash, spanish.Version, spanish.ContentHash)
	}
}

// The text is placeholder and says so where a reader can see it, not only in a
// migration comment. This test is here to fail loudly if the real legal drop
// ever lands WITHOUT a new Policy Version — at which point it should be deleted
// in the same commit that publishes edition 1.
func TestPrivacyPolicyStillShowsItsPlaceholders(t *testing.T) {
	env := setupTest(t)

	for _, locale := range []string{"en", "es"} {
		_, _, payload := getPrivacyPolicy(t, env, locale)
		for _, marker := range []string{"[DIRECCIÓN]", "[AUTORIDAD DE PROTECCIÓN DE DATOS]"} {
			if !strings.Contains(payload.BodyMarkdown, marker) {
				t.Errorf("locale %s: the %s placeholder is gone from the published body", locale, marker)
			}
		}
	}
}

// A language-and-region tag names its language. "es-EC" is Spanish, and a
// Storefront that ever forwards one must not get a 404 for it
// (platform.ParseLocale).
func TestPrivacyPolicyAcceptsALanguageAndRegionTag(t *testing.T) {
	env := setupTest(t)

	resp, body, payload := getPrivacyPolicy(t, env, "es-EC")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if payload.Locale != "es" {
		t.Errorf("locale = %q, want es", payload.Locale)
	}
}

// A language the policy is not published in is refused, never answered in
// another one: a notice the reader cannot read, served under their own
// language's address, would be worse than an honest absence.
func TestPrivacyPolicyRefusesAnUnpublishedLanguage(t *testing.T) {
	env := setupTest(t)

	resp, body, _ := getPrivacyPolicy(t, env, "fr")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "POLICY_LOCALE_NOT_PUBLISHED" {
		t.Fatalf("error = %+v, want POLICY_LOCALE_NOT_PUBLISHED", body.Error)
	}
	if len(body.Data) != 0 && string(body.Data) != "null" {
		t.Errorf("data on a failure: %s", string(body.Data))
	}
}
