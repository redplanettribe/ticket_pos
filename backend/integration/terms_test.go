package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
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
	// AdulthoodDeclarationLabel is a POINTER on purpose: the field is absent
	// from the payload when the edition in effect does not ask for an Adulthood
	// Declaration (ADR 0069), and "the edition does not ask" must be
	// distinguishable here from "it asks with nothing written beside the box".
	// A client draws the second box iff this arrives.
	AdulthoodDeclarationLabel *string `json:"adulthood_declaration_label"`
	BodyMarkdown              string  `json:"body_markdown"`
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

// The fingerprint on the wire is the fingerprint of the text on the wire — the
// whole evidentiary chain in one assertion, recomputed from what was actually
// served in both languages (#558; privacy_policy_test.go's policyHashOf).
func TestTermsContentHashMatchesTheServedText(t *testing.T) {
	env := setupTest(t)

	_, _, spanish := getTerms(t, env, "es")
	_, _, english := getTerms(t, env, "en")

	var artifacts []legal.Artifact
	for _, served := range []termsPayload{english, spanish} {
		locale := platform.Locale(served.Locale)
		for i, body := range []string{served.AcceptanceLabel, served.BodyMarkdown} {
			artifacts = append(artifacts, legal.Artifact{Locale: locale, Ordinal: i + 1, Body: body})
		}
	}
	if want := legal.ContentHash(artifacts); spanish.ContentHash != want {
		t.Errorf("content_hash = %q, but the served text hashes to %q", spanish.ContentHash, want)
	}
}

// The Terms' bytes come from rows too, and the reader cannot tell (#558).
func TestTermsAreServedFromTheStoredArtifacts(t *testing.T) {
	env := setupTest(t)

	_, _, payload := getTerms(t, env, "es")

	var stored string
	err := env.db.QueryRow(`
		SELECT a.body
		FROM terms_version_artifacts a
		JOIN terms_versions v ON v.id = a.version_id
		WHERE v.label = $1 AND a.locale = 'es' AND a.slug = 'terms'`, payload.Version).Scan(&stored)
	if err != nil {
		t.Fatalf("read the stored terms body: %v", err)
	}
	if stored != payload.BodyMarkdown {
		t.Error("the served body is not the stored row")
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

// THE EDITION IN EFFECT DOES NOT ASK, so the label is not on the wire — in
// either language (#585, ADR 0069). This is the assertion that says this binary
// can ship before an operator publishes anything: it knows the Artifact and
// does not require it, and the Storefront draws no second box today.
func TestTheTermsOmitTheAdulthoodDeclarationLabelWhenTheEditionDoesNotAsk(t *testing.T) {
	env := setupTest(t)

	for _, locale := range []string{"es", "en"} {
		resp, body, payload := getTerms(t, env, locale)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d error=%+v", locale, resp.StatusCode, body.Error)
		}
		if payload.AdulthoodDeclarationLabel != nil {
			t.Errorf("the %s terms carry an adulthood declaration label the current edition does not publish: %q",
				locale, *payload.AdulthoodDeclarationLabel)
		}
	}
}

// AND THE EDITION THAT DOES ASK SERVES THE WORDS, in both languages, with no
// deploy between the two states: the whole of introducing the box is publishing
// an edition that carries the Artifact (ADR 0069). Publishing here is the raw
// insert publishTermsVersion uses — the ceremony that produces one in
// production is the Legal Center's, and is not what this test is about.
func TestTheTermsCarryTheAdulthoodDeclarationLabelWhenTheEditionAsks(t *testing.T) {
	env := setupTest(t)

	id := publishTermsVersion(t, env, 2, 0)
	if _, err := env.db.Exec(`
		INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
		VALUES ($1, 'en', 'label-adulthood-declaration', 3, 'I declare that I am eighteen years of age or older.'),
		       ($1, 'es', 'label-adulthood-declaration', 3, 'Declaro ser mayor de edad.')
	`, id); err != nil {
		t.Fatalf("publish the adulthood declaration label: %v", err)
	}

	for locale, want := range map[string]string{
		"es": "Declaro ser mayor de edad.",
		"en": "I declare that I am eighteen years of age or older.",
	} {
		resp, body, payload := getTerms(t, env, locale)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d error=%+v", locale, resp.StatusCode, body.Error)
		}
		if payload.Version != "2" {
			t.Fatalf("%s: version = %q, want the edition just published", locale, payload.Version)
		}
		if payload.AdulthoodDeclarationLabel == nil {
			t.Errorf("the %s terms omit the adulthood declaration label the edition publishes", locale)
			continue
		}
		if *payload.AdulthoodDeclarationLabel != want {
			t.Errorf("the %s adulthood declaration label = %q, want %q", locale, *payload.AdulthoodDeclarationLabel, want)
		}
		// The rest of the document is untouched by the new Artifact: an edition
		// that asks is still a whole contract, not a checkbox with a page
		// attached.
		if payload.AcceptanceLabel == "" || payload.BodyMarkdown == "" {
			t.Errorf("the %s terms lost text to the new artifact", locale)
		}
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
