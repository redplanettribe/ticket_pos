package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/terms"
)

// The public Términos y Condiciones read (#535, parent #533): the current Terms
// Version served with its edition label and fingerprint, over HTTP.
//
// Like the privacy policy tests, what these are really about is the tie
// between the text and its fingerprint — the seed drift test in
// internal/consent/terms pins the migration against the embedded text, and
// these pin what actually comes back on the wire. Plus the two rulings that are
// the Terms' own: both published languages are ONE edition under ONE
// fingerprint, and the Spanish is the legally prevailing text (§37, ADR 0066)
// with the English carrying a courtesy-translation notice that says so.

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

// The English reader gets the English text — a different document, the SAME
// edition and the SAME fingerprint. That pairing is the whole evidentiary
// claim: a person who accepted after reading the translation accepted the
// edition whose hash is recorded against them, and the text they were shown is
// inside that hash.
func TestTheEnglishTermsAreTheSameEditionAsTheSpanish(t *testing.T) {
	env := setupTest(t)

	_, _, spanish := getTerms(t, env, "es")

	resp, body, english := getTerms(t, env, "en")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if english.Locale != "en" {
		t.Errorf("payload locale = %q, want en", english.Locale)
	}
	if english.BodyMarkdown == spanish.BodyMarkdown {
		t.Error("the english body is the spanish one; no translation is being served")
	}
	if english.AcceptanceLabel == "" {
		t.Error("the english acceptance_label is empty")
	}
	if english.Version != spanish.Version {
		t.Errorf("english version = %q, spanish = %q; they must be one edition", english.Version, spanish.Version)
	}
	if english.ContentHash != spanish.ContentHash {
		t.Errorf("english content_hash = %q, spanish = %q; one edition has one fingerprint",
			english.ContentHash, spanish.ContentHash)
	}
	if !strings.Contains(english.BodyMarkdown, "Courtesy translation") {
		t.Error("the english body does not say it is a translation")
	}
}

// A language the Terms are not published in is a 404, the privacy policy
// endpoint's rule: answering it with either published document would put a
// contract nobody asked for under that language's address.
func TestTermsRefuseAnUnpublishedLocale(t *testing.T) {
	env := setupTest(t)

	resp, body, _ := getTerms(t, env, "fr")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TERMS_LOCALE_NOT_PUBLISHED" {
		t.Errorf("error = %+v, want TERMS_LOCALE_NOT_PUBLISHED", body.Error)
	}
}

// A language-and-region tag names its language: "es-EC" is Spanish, and it is
// answered with the Spanish document rather than refused (platform.ParseLocale,
// which every Locale in this system is read through). What is refused is a
// language nothing here is written in, above.
func TestTermsAcceptARegionTaggedLocale(t *testing.T) {
	env := setupTest(t)

	resp, body, payload := getTerms(t, env, "es-EC")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if payload.Locale != "es" {
		t.Errorf("payload locale = %q, want es", payload.Locale)
	}
}
