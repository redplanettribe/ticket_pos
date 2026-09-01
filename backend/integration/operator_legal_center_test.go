package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Legal Center's drafting half (#561, spec #556): one mutable draft per
// legal document, held by the API.
//
// What this slice must prove is the reason the draft is server-side at all — it
// SURVIVES THE BROWSER. A draft in local storage would pass every unit test and
// still lose an afternoon to a closed tab, so the assertions here are all of the
// form "write it, forget everything, read it back".
//
// It also pins the three things a later publication depends on: the artifact
// order is the ordinal (the fingerprint preimage's order, #541), the
// published-language set is EXPLICIT rather than inferred from which cells are
// filled, and an incomplete draft is SAVED rather than refused — completeness
// refuses a publication (#563), never an afternoon's work.

const operatorLegalPolicyPath = "/api/v1/operator/legal/documents/policy"

type legalArtifactView struct {
	Slug    string            `json:"slug"`
	Ordinal int               `json:"ordinal"`
	Bodies  map[string]string `json:"bodies"`
}

type legalEditionView struct {
	VersionID     string              `json:"version_id"`
	Label         string              `json:"label"`
	EffectiveDate string              `json:"effective_date"`
	ContentHash   string              `json:"content_hash"`
	Locales       []string            `json:"locales"`
	Artifacts     []legalArtifactView `json:"artifacts"`
}

type legalDraftView struct {
	Stored           bool                `json:"stored"`
	BaseVersionID    string              `json:"base_version_id"`
	BaseIsCurrent    bool                `json:"base_is_current"`
	PublishedLocales []string            `json:"published_locales"`
	Artifacts        []legalArtifactView `json:"artifacts"`
	UpdatedBy        string              `json:"updated_by"`
	UpdatedAt        *string             `json:"updated_at"`
}

type legalWorkspaceView struct {
	Document         string           `json:"document"`
	SupportedLocales []string         `json:"supported_locales"`
	Published        legalEditionView `json:"published"`
	Draft            legalDraftView   `json:"draft"`
}

func legalWorkspace(t *testing.T, env *testEnv, sessionID, path string) legalWorkspaceView {
	t.Helper()
	var workspace legalWorkspaceView
	operatorGetOK(t, env, sessionID, path, &workspace)
	return workspace
}

func saveLegalDraft(t *testing.T, env *testEnv, sessionID string, body map[string]any) legalWorkspaceView {
	t.Helper()
	resp, envelopeBody := env.put(t, operatorLegalPolicyPath+"/draft", body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save draft status=%d error=%+v", resp.StatusCode, envelopeBody.Error)
	}
	var workspace legalWorkspaceView
	if err := json.Unmarshal(envelopeBody.Data, &workspace); err != nil {
		t.Fatalf("decode saved draft: %v", err)
	}
	return workspace
}

// draftBody turns the artifact list into the request shape. NO ORDINALS TRAVEL:
// the position in the list is the ordinal, which is the whole reason the two
// cannot disagree.
func draftBody(locales []string, artifacts []legalArtifactView) map[string]any {
	rows := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		rows = append(rows, map[string]any{"slug": artifact.Slug, "bodies": artifact.Bodies})
	}
	return map[string]any{"published_locales": locales, "artifacts": rows}
}

func slugsOf(artifacts []legalArtifactView) []string {
	slugs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		slugs = append(slugs, artifact.Slug)
	}
	return slugs
}

// TestOperatorLegalDraftSurvivesTheBrowser is the point of the feature: a draft
// written now is the same draft read back by a session that knows nothing about
// the one that wrote it, and discarding it restores the published edition.
func TestOperatorLegalDraftSurvivesTheBrowser(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// With nothing saved, the draft IS the published edition — the same answer a
	// discard produces, so the editor never has a third state to render.
	fresh := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	if fresh.Document != "policy" {
		t.Fatalf("document = %q; want policy", fresh.Document)
	}
	if len(fresh.SupportedLocales) != 2 || fresh.SupportedLocales[0] != "en" || fresh.SupportedLocales[1] != "es" {
		t.Fatalf("supported locales = %v; want the platform's app locales in preimage order", fresh.SupportedLocales)
	}
	if fresh.Draft.Stored {
		t.Fatalf("a document nobody has drafted reports stored=true")
	}
	if fresh.Draft.UpdatedAt != nil || fresh.Draft.UpdatedBy != "" {
		t.Fatalf("an unsaved draft names an author: %+v", fresh.Draft)
	}
	if fresh.Draft.BaseVersionID != fresh.Published.VersionID || !fresh.Draft.BaseIsCurrent {
		t.Fatalf("unsaved draft's base = %q; want the current edition %q", fresh.Draft.BaseVersionID, fresh.Published.VersionID)
	}
	if len(fresh.Published.Artifacts) == 0 || fresh.Published.ContentHash == "" {
		t.Fatalf("published edition = %+v; want the seeded policy with its fingerprint", fresh.Published)
	}
	published := slugsOf(fresh.Published.Artifacts)
	if got := slugsOf(fresh.Draft.Artifacts); len(got) != len(published) {
		t.Fatalf("unsaved draft's artifacts = %v; want a copy of the published edition %v", got, published)
	}

	// Edit one cell in English, leave the Spanish alone.
	edited := make([]legalArtifactView, len(fresh.Draft.Artifacts))
	copy(edited, fresh.Draft.Artifacts)
	for i := range edited {
		bodies := map[string]string{}
		for locale, body := range edited[i].Bodies {
			bodies[locale] = body
		}
		if edited[i].Slug == published[0] {
			bodies["en"] = "Rewritten in the Legal Center."
		}
		edited[i].Bodies = bodies
	}
	saved := saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, edited))
	if !saved.Draft.Stored || saved.Draft.UpdatedBy != "operator@example.com" || saved.Draft.UpdatedAt == nil {
		t.Fatalf("saved draft = %+v; want it stored and attributed to the session", saved.Draft)
	}

	// THE RELOAD. A second session reads the same draft: nothing about it lived
	// in the browser that wrote it.
	otherSessionID := operatorSession(t, env, "colleague@example.com")
	reloaded := legalWorkspace(t, env, otherSessionID, operatorLegalPolicyPath)
	if !reloaded.Draft.Stored {
		t.Fatalf("the draft did not survive the session that wrote it")
	}
	if got := slugsOf(reloaded.Draft.Artifacts); len(got) != len(published) {
		t.Fatalf("reloaded slugs = %v; want %v", got, published)
	}
	if reloaded.Draft.Artifacts[0].Bodies["en"] != "Rewritten in the Legal Center." {
		t.Fatalf("reloaded first artifact = %+v; want the edit", reloaded.Draft.Artifacts[0])
	}
	// The edit is a DRAFT and nothing more: the published edition is untouched,
	// fingerprint included.
	if reloaded.Published.ContentHash != fresh.Published.ContentHash {
		t.Fatalf("saving a draft moved the published fingerprint from %q to %q",
			fresh.Published.ContentHash, reloaded.Published.ContentHash)
	}
	if reloaded.Published.Artifacts[0].Bodies["en"] == "Rewritten in the Legal Center." {
		t.Fatalf("saving a draft published it")
	}

	// The discard: back to the published edition, and a second discard is still
	// a success because what the caller asked for is what they have.
	for range 2 {
		resp, body := env.deleteJSON(t, operatorLegalPolicyPath+"/draft", nil, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("discard status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	after := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	if after.Draft.Stored {
		t.Fatalf("a discarded draft is still stored")
	}
	if after.Draft.Artifacts[0].Bodies["en"] != after.Published.Artifacts[0].Bodies["en"] {
		t.Fatalf("a discarded draft did not return to the published edition")
	}
}

// TestOperatorLegalDraftHoldsAnIncompleteEdition: adding an artifact whose
// Spanish has not been written, and dropping a language, are both ordinary
// SAVES. The refusals belong to publish (#563), not to somebody's afternoon.
func TestOperatorLegalDraftHoldsAnIncompleteEdition(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	fresh := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	artifacts := append([]legalArtifactView{}, fresh.Draft.Artifacts...)
	// A new optional consent box, its English written and its Spanish not.
	artifacts = append(artifacts, legalArtifactView{
		Slug:   "label-analytics-consent",
		Bodies: map[string]string{"en": "I agree to analytics.", "es": "   "},
	})

	saved := saveLegalDraft(t, env, sessionID, draftBody([]string{"en"}, artifacts))
	if len(saved.Draft.PublishedLocales) != 1 || saved.Draft.PublishedLocales[0] != "en" {
		t.Fatalf("published locales = %v; want the explicit set the operator stated", saved.Draft.PublishedLocales)
	}

	var added *legalArtifactView
	for i := range saved.Draft.Artifacts {
		if saved.Draft.Artifacts[i].Slug == "label-analytics-consent" {
			added = &saved.Draft.Artifacts[i]
		}
	}
	if added == nil {
		t.Fatalf("the added artifact is missing from %v", slugsOf(saved.Draft.Artifacts))
	}
	// Its position in the list it was sent in is its ordinal, and a whitespace
	// cell is stored as ABSENT so that "emptied" and "never written" cannot
	// drift apart between the editor and the completeness rule.
	if added.Ordinal != len(artifacts) {
		t.Fatalf("added artifact's ordinal = %d; want its position %d", added.Ordinal, len(artifacts))
	}
	if _, written := added.Bodies["es"]; written {
		t.Fatalf("a whitespace-only cell was stored as text: %+v", added.Bodies)
	}

	// Removing an artifact is nothing more than saving a shorter list.
	shorter := saved.Draft.Artifacts[:len(saved.Draft.Artifacts)-2]
	afterRemoval := saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, shorter))
	if len(afterRemoval.Draft.Artifacts) != len(shorter) {
		t.Fatalf("after removal = %v; want %v", slugsOf(afterRemoval.Draft.Artifacts), slugsOf(shorter))
	}
	for i, artifact := range afterRemoval.Draft.Artifacts {
		if artifact.Ordinal != i+1 {
			t.Fatalf("ordinals after a removal = %+v; want 1..n renumbered from the list", afterRemoval.Draft.Artifacts)
		}
	}
}

// TestOperatorLegalDraftRefusesWhatItCannotStore: the shape refusals. Each one
// is a request the platform could not store coherently — never a draft that is
// merely unfinished.
func TestOperatorLegalDraftRefusesWhatItCannotStore(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	headers := authHeader(sessionID)

	one := []legalArtifactView{{Slug: "policy", Bodies: map[string]string{"en": "text"}}}

	cases := []struct {
		name string
		path string
		body map[string]any
		code string
	}{
		{
			name: "a document that is not one of the two",
			path: "/api/v1/operator/legal/documents/refund-policy/draft",
			body: draftBody([]string{"en"}, one),
			code: "LEGAL_DOCUMENT_NOT_FOUND",
		},
		{
			name: "no language at all",
			path: operatorLegalPolicyPath + "/draft",
			body: draftBody([]string{}, one),
			code: "LEGAL_DRAFT_LOCALES_REQUIRED",
		},
		{
			name: "a language this platform does not serve",
			path: operatorLegalPolicyPath + "/draft",
			body: draftBody([]string{"en", "fr"}, one),
			code: "LEGAL_DRAFT_LOCALE_UNSUPPORTED",
		},
		{
			name: "an artifact with no slug",
			path: operatorLegalPolicyPath + "/draft",
			body: draftBody([]string{"en"}, []legalArtifactView{{Slug: "  ", Bodies: map[string]string{"en": "text"}}}),
			code: "LEGAL_DRAFT_SLUG_REQUIRED",
		},
		{
			name: "the same slug twice",
			path: operatorLegalPolicyPath + "/draft",
			body: draftBody([]string{"en"}, []legalArtifactView{one[0], one[0]}),
			code: "LEGAL_DRAFT_DUPLICATE_SLUG",
		},
	}

	for _, testCase := range cases {
		resp, body := env.put(t, testCase.path, testCase.body, headers)
		if body.Error == nil || body.Error.Code != testCase.code {
			t.Fatalf("%s: error=%+v; want %s", testCase.name, body.Error, testCase.code)
		}
		wantStatus := http.StatusBadRequest
		if testCase.code == "LEGAL_DOCUMENT_NOT_FOUND" {
			wantStatus = http.StatusNotFound
		}
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s: status=%d; want %d", testCase.name, resp.StatusCode, wantStatus)
		}
	}

	// Nothing was stored by any of them.
	if workspace := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath); workspace.Draft.Stored {
		t.Fatalf("a refused save left a draft behind")
	}

	// The read refuses an unknown document too, and refuses it the same way.
	resp, body := env.get(t, "/api/v1/operator/legal/documents/refund-policy", headers)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "LEGAL_DOCUMENT_NOT_FOUND" {
		t.Fatalf("GET an unknown document: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}
