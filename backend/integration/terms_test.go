package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/terms"
)

// The public Términos y Condiciones read (#535, parent #533): the current Terms
// Version served with its edition label and fingerprint, over HTTP.
//
// Like the privacy policy tests, what these are really about is the tie
// between the text and its fingerprint — the seed drift test in
// internal/consent/terms pins the migration against the embedded text, and
// these pin what actually comes back on the wire. Plus the one ruling that is
// the Terms' own: the Spanish document is served for ANY locale, because it is
// the single legally prevailing text (§37, ADR 0066) and an English reader
// gets the operative contract rather than a 404.

type termsPayload struct {
	Version         string `json:"version"`
	EffectiveDate   string `json:"effective_date"`
	ContentHash     string `json:"content_hash"`
	Locale          string `json:"locale"`
	AcceptanceLabel string `json:"acceptance_label"`
	BodyMarkdown    string `json:"body_markdown"`
}

func getTerms(t *testing.T, env *testEnv, locale string) (*http.Response, envelope, termsPayload) {
	t.Helper()

	resp, body := env.get(t, "/api/v1/public/terms/"+locale, nil)
	var payload termsPayload
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body.Data, &payload); err != nil {
			t.Fatalf("decode terms: %v", err)
		}
	}
	return resp, body, payload
}

// The feature, in one request: an unauthenticated caller reads the whole
// current edition.
func TestTermsArePublicAndComplete(t *testing.T) {
	env := setupTest(t)

	resp, body, payload := getTerms(t, env, "es")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error on success: %+v", body.Error)
	}
	if body.RequestID == "" {
		t.Error("no request id on the envelope")
	}

	if payload.Version != "1" {
		t.Errorf("version = %q, want the current edition", payload.Version)
	}
	if payload.EffectiveDate != "2026-08-30" {
		t.Errorf("effective_date = %q, want 2026-08-30", payload.EffectiveDate)
	}
	if payload.Locale != "es" {
		t.Errorf("locale = %q, want es", payload.Locale)
	}
	if payload.AcceptanceLabel == "" {
		t.Error("acceptance_label is empty")
	}
	if payload.BodyMarkdown == "" {
		t.Error("body_markdown is empty")
	}
}

// The fingerprint on the wire is the fingerprint of the embedded text — the
// whole evidentiary chain in one assertion.
func TestTermsContentHashMatchesTheEmbeddedArtifacts(t *testing.T) {
	env := setupTest(t)

	_, _, payload := getTerms(t, env, "es")
	if want := terms.ContentHash(); payload.ContentHash != want {
		t.Errorf("content_hash = %q, want the embedded artifacts' %q", payload.ContentHash, want)
	}
}

// Any locale is served the Spanish document — the English page and even a
// locale the platform does not publish at all. No fallback dance, no 404: there
// is exactly one legally operative text (§37), and this endpoint's answer is
// it, whoever asks.
func TestTermsAreServedInSpanishForAnyLocale(t *testing.T) {
	env := setupTest(t)

	_, _, reference := getTerms(t, env, "es")

	for _, locale := range []string{"en", "fr", "es-EC"} {
		resp, body, payload := getTerms(t, env, locale)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("locale %s: status=%d error=%+v", locale, resp.StatusCode, body.Error)
		}
		if payload.Locale != "es" {
			t.Errorf("locale %s: payload locale = %q, want es", locale, payload.Locale)
		}
		if payload.BodyMarkdown != reference.BodyMarkdown {
			t.Errorf("locale %s: body differs from the Spanish document", locale)
		}
		if payload.ContentHash != reference.ContentHash {
			t.Errorf("locale %s: content_hash differs from the Spanish document's", locale)
		}
	}
}
