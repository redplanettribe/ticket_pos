package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Publishing (#563, spec #556, ADR 0067): the one act in the Legal Center that
// a reader can see.
//
// EVERY REFUSAL IS TESTED AT THIS SEAM AND NOT ONLY IN THE BROWSER, which is the
// whole reason this slice exists. The screen disables the correction link, hides
// the date box on a correction and greys out the protected language — and none
// of that is a guarantee, because a client can decline to check anything. What
// the platform can guarantee is what this file asserts: that the HTTP call
// refuses.
//
// It also pins the two shapes of the act. A GATING publication takes the next
// generation, waits a night and carries a headcount; a CORRECTION takes a flat
// revision, is immediate, and re-gates nobody. And it pins the thing neither may
// ever do: no row an acceptance points at is mutated, so the edition that was
// corrected keeps its own id, its own label and its own fingerprint.

const operatorLegalTermsPath = "/api/v1/operator/legal/documents/terms"

// legalPublishPlanView is the publish half of the workspace: what the button
// would do if it were pressed now.
type legalPublishPlanView struct {
	GatingLabel           string `json:"gating_label"`
	CorrectionLabel       string `json:"correction_label"`
	Headcount             int    `json:"headcount"`
	CanPublish            bool   `json:"can_publish"`
	Complete              bool   `json:"complete"`
	Structural            bool   `json:"structural"`
	LocaleSetChanged      bool   `json:"locale_set_changed"`
	EmptyDiff             bool   `json:"empty_diff"`
	CanCorrect            bool   `json:"can_correct"`
	ProtectedLocale       string `json:"protected_locale"`
	ProtectedLocaleKept   bool   `json:"protected_locale_kept"`
	EarliestEffectiveDate string `json:"earliest_effective_date"`
	DiffSummary           string `json:"diff_summary"`
}

// legalPublishDraftView is the draft as this slice reads it: what is in it, and
// what is still to be looked at. Its own struct rather than an addition to
// operator_legal_center_test.go's legalDraftView, so the three Legal Center
// slices' assertions stay independent.
type legalPublishDraftView struct {
	Stored           bool                `json:"stored"`
	PublishedLocales []string            `json:"published_locales"`
	Artifacts        []legalArtifactView `json:"artifacts"`
	PreviewGaps      []struct {
		Slug   string `json:"slug"`
		Locale string `json:"locale"`
	} `json:"preview_gaps"`
	PreviewedAll bool `json:"previewed_all"`
	SeenDiff     bool `json:"seen_diff"`
}

type legalPublishWorkspaceView struct {
	Published legalEditionView      `json:"published"`
	Draft     legalPublishDraftView `json:"draft"`
	Publish   legalPublishPlanView  `json:"publish"`
}

func legalPublishWorkspace(t *testing.T, env *testEnv, sessionID, path string) legalPublishWorkspaceView {
	t.Helper()
	var workspace legalPublishWorkspaceView
	operatorGetOK(t, env, sessionID, path, &workspace)
	return workspace
}

// saveLegalDraftAt is saveLegalDraft for either document.
func saveLegalDraftAt(t *testing.T, env *testEnv, sessionID, path string, body map[string]any) legalPublishWorkspaceView {
	t.Helper()
	resp, envelopeBody := env.put(t, path+"/draft", body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save draft status=%d error=%+v", resp.StatusCode, envelopeBody.Error)
	}
	var workspace legalPublishWorkspaceView
	if err := json.Unmarshal(envelopeBody.Data, &workspace); err != nil {
		t.Fatalf("decode saved draft: %v", err)
	}
	return workspace
}

// reviewWholeDraft clears the two review gates (#562) the honest way: it
// previews every cell the draft still owes and then looks at the diff. Nothing
// here reaches around the rules; if a gate did not clear, the publish that
// follows will say so.
func reviewWholeDraft(t *testing.T, env *testEnv, sessionID, path string) legalPublishWorkspaceView {
	t.Helper()
	workspace := legalPublishWorkspace(t, env, sessionID, path)
	for _, gap := range workspace.Draft.PreviewGaps {
		resp, body := env.post(t, path+"/draft/previews",
			map[string]any{"slug": gap.Slug, "locale": gap.Locale}, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview %s/%s status=%d error=%+v", gap.Slug, gap.Locale, resp.StatusCode, body.Error)
		}
	}
	resp, body := env.post(t, path+"/draft/diff-seen", map[string]any{}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("see diff status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var seen legalPublishWorkspaceView
	if err := json.Unmarshal(body.Data, &seen); err != nil {
		t.Fatalf("decode diff-seen: %v", err)
	}
	return seen
}

func publishLegal(t *testing.T, env *testEnv, sessionID, path string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, path+"/publications", body, authHeader(sessionID))
}

func publishLegalOK(t *testing.T, env *testEnv, sessionID, path string, body map[string]any) legalPublishWorkspaceView {
	t.Helper()
	resp, envelopeBody := publishLegal(t, env, sessionID, path, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish status=%d error=%+v", resp.StatusCode, envelopeBody.Error)
	}
	var workspace legalPublishWorkspaceView
	if err := json.Unmarshal(envelopeBody.Data, &workspace); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	return workspace
}

// refusedWith asserts the seam refused, with the code and status the rule is
// written under. The CODE is what is asserted and not the sentence: the sentence
// is copy, the code is the contract.
func refusedWith(t *testing.T, resp *http.Response, body envelope, status int, code string) {
	t.Helper()
	if resp.StatusCode != status || body.Error == nil || body.Error.Code != code {
		t.Fatalf("status=%d error=%+v; want %d %s", resp.StatusCode, body.Error, status, code)
	}
}

// rewordOne returns the draft's artifacts with one language of one artifact
// rewritten — the smallest honest change there is.
func rewordOne(artifacts []legalArtifactView, slug, locale, text string) []legalArtifactView {
	edited := make([]legalArtifactView, 0, len(artifacts))
	for _, artifact := range artifacts {
		bodies := map[string]string{}
		for token, body := range artifact.Bodies {
			bodies[token] = body
		}
		if artifact.Slug == slug {
			bodies[locale] = text
		}
		edited = append(edited, legalArtifactView{Slug: artifact.Slug, Bodies: bodies})
	}
	return edited
}

// TestOperatorLegalPublishesAGatingEdition walks the whole default path: write,
// look, publish, and find the consequence on the row.
func TestOperatorLegalPublishesAGatingEdition(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	fresh := legalPublishWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	// The plan names the label the button would create, BEFORE anything is
	// written. The Privacy Policy is seeded as `0-placeholder` and `1`
	// (migrations 060 and 066), so the next gating edition is `2` — and choosing
	// the correction visibly turns that `2` into `1.1`, which is the whole point
	// of naming both.
	if fresh.Publish.GatingLabel != "2" || fresh.Publish.CorrectionLabel != "1.1" {
		t.Fatalf("labels = %q / %q; want the next generation and a flat revision",
			fresh.Publish.GatingLabel, fresh.Publish.CorrectionLabel)
	}
	if fresh.Publish.ProtectedLocale != "es" || !fresh.Publish.ProtectedLocaleKept {
		t.Fatalf("protected locale = %+v; want Spanish, kept", fresh.Publish)
	}
	if fresh.Publish.CanPublish {
		t.Fatalf("a draft nobody has looked at reports can_publish")
	}

	// A Customer standing on the current edition: the person this publication
	// will re-gate, and the number that must reach the row.
	if _, err := env.db.ExecContext(t.Context(), `
		INSERT INTO customers (email, first_name, last_name, policy_version_id, policy_accepted_at)
		VALUES ('regated@example.com', 'Re', 'Gated', $1, now())
	`, fresh.Published.VersionID); err != nil {
		t.Fatalf("seed an accepting customer: %v", err)
	}

	saved := saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "policy", "en", "The policy, restated.")))
	if saved.Publish.Headcount != 1 {
		t.Fatalf("headcount = %d; want the one Customer standing on the current edition", saved.Publish.Headcount)
	}
	if saved.Publish.EmptyDiff || saved.Publish.Structural || saved.Publish.LocaleSetChanged {
		t.Fatalf("a reworded draft reads as %+v", saved.Publish)
	}

	reviewed := reviewWholeDraft(t, env, sessionID, operatorLegalPolicyPath)
	if !reviewed.Publish.CanPublish || !reviewed.Publish.CanCorrect {
		t.Fatalf("after previewing everything and seeing the diff: %+v", reviewed.Publish)
	}

	// "Now" is refused, and so is today: an irreversible re-gate gets a night.
	resp, body := publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{"kind": "edition"})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_GATING_EFFECTIVE_DATE_TOO_SOON")

	published := publishLegalOK(t, env, sessionID, operatorLegalPolicyPath, map[string]any{
		"kind":           "edition",
		"effective_date": reviewed.Publish.EarliestEffectiveDate,
	})

	// The draft became the edition, so there is no draft any more: the editor is
	// back in the one state it has for "nothing in progress".
	if published.Draft.Stored {
		t.Fatalf("the draft survived its own publication")
	}

	// The row carries the whole provenance, including the headcount AS IT STOOD
	// ON THE BUTTON — the proof that the consequence was shown.
	var (
		label                string
		generation           int
		revision             int
		effective            time.Time
		publishedBy, summary string
		headcount            int
		reason               *string
	)
	if err := env.db.QueryRowContext(t.Context(), `
		SELECT label, generation, revision, effective_date, published_by, publish_diff_summary,
		       regated_headcount, correction_reason
		FROM policy_versions WHERE label = '2'
	`).Scan(&label, &generation, &revision, &effective, &publishedBy, &summary, &headcount, &reason); err != nil {
		t.Fatalf("read the published edition: %v", err)
	}
	if generation != 2 || revision != 0 {
		t.Fatalf("lineage = %d.%d; want the next generation at revision 0", generation, revision)
	}
	if effective.Format("2006-01-02") != reviewed.Publish.EarliestEffectiveDate {
		t.Fatalf("effective date = %s; want %s", effective.Format("2006-01-02"), reviewed.Publish.EarliestEffectiveDate)
	}
	if publishedBy != "operator@example.com" || headcount != 1 || summary == "" {
		t.Fatalf("provenance = %q / %d / %q", publishedBy, headcount, summary)
	}
	if reason != nil {
		t.Fatalf("a gating edition carries a correction reason: %q", *reason)
	}

	// NOTHING WAS MUTATED. The edition that was superseded keeps its own id, its
	// own label and its own bytes, because acceptances point at them.
	var supersededHash string
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT content_hash FROM policy_versions WHERE id = $1`, fresh.Published.VersionID).Scan(&supersededHash); err != nil {
		t.Fatalf("the superseded edition went missing: %v", err)
	}
	if supersededHash != fresh.Published.ContentHash {
		t.Fatalf("the superseded edition's fingerprint moved: %q -> %q", fresh.Published.ContentHash, supersededHash)
	}
	// And the Customer's acceptance still names the row it always named.
	var standing string
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT policy_version_id::text FROM customers WHERE email = 'regated@example.com'`).Scan(&standing); err != nil {
		t.Fatalf("read the acceptance: %v", err)
	}
	if standing != fresh.Published.VersionID {
		t.Fatalf("the acceptance was rewritten: %q -> %q", fresh.Published.VersionID, standing)
	}
}

// TestOperatorLegalPublishesACorrectionThatRegatesNobody is the other act: a
// flat revision, immediate, with a reason, moving nobody.
func TestOperatorLegalPublishesACorrectionThatRegatesNobody(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	fresh := legalPublishWorkspace(t, env, sessionID, operatorLegalTermsPath)
	if _, err := env.db.ExecContext(t.Context(), `
		INSERT INTO customers (email, first_name, last_name, terms_version_id, terms_accepted_at)
		VALUES ('held@example.com', 'Held', 'Steady', $1, now())
	`, fresh.Published.VersionID); err != nil {
		t.Fatalf("seed an accepting customer: %v", err)
	}

	saveLegalDraftAt(t, env, sessionID, operatorLegalTermsPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "terms", "en", "The terms, with the typo fixed.")))
	reviewed := reviewWholeDraft(t, env, sessionID, operatorLegalTermsPath)
	if !reviewed.Publish.CanCorrect {
		t.Fatalf("a reworded, fully reviewed draft cannot be corrected: %+v", reviewed.Publish)
	}

	// A correction NAMES NO DATE. It takes effect immediately, so there is
	// nothing to schedule — and a date somebody typed is refused rather than
	// quietly dropped.
	resp, body := publishLegal(t, env, sessionID, operatorLegalTermsPath, map[string]any{
		"kind": "correction", "reason": "The English text said 'buyer' where it meant 'holder'.",
		"effective_date": "2099-01-01",
	})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_CORRECTION_EFFECTIVE_DATE_REFUSED")

	// And it needs a reason of meaningful length: the diff says what moved, the
	// reason says what it was for, and that is the one thing the bytes cannot say.
	for _, reason := range []string{"", "typo"} {
		resp, body := publishLegal(t, env, sessionID, operatorLegalTermsPath,
			map[string]any{"kind": "correction", "reason": reason})
		refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_CORRECTION_REASON_REQUIRED")
	}

	published := publishLegalOK(t, env, sessionID, operatorLegalTermsPath, map[string]any{
		"kind": "correction", "reason": "The English text said 'buyer' where it meant 'holder'.",
	})
	// The Terms' seeded edition is `1`, so the correction is `1.1` — FLAT, and
	// a correction to it would be `1.2`, never `1.1.1`.
	if published.Published.Label != "1.1" {
		t.Fatalf("published label = %q; want the flat correction 1.1", published.Published.Label)
	}

	var (
		revision  int
		headcount int
		reason    string
	)
	if err := env.db.QueryRowContext(t.Context(), `
		SELECT revision, regated_headcount, correction_reason
		FROM terms_versions WHERE label = '1.1'
	`).Scan(&revision, &headcount, &reason); err != nil {
		t.Fatalf("read the correction: %v", err)
	}
	if revision != 1 || headcount != 0 || reason == "" {
		t.Fatalf("correction row: revision=%d headcount=%d reason=%q", revision, headcount, reason)
	}
	// IMMEDIATE, and the observable form of "immediate" is that the correction is
	// what a reader gets NOW — which the label on the response above already is,
	// because the workspace reports the CURRENT edition.
	if published.Published.VersionID == fresh.Published.VersionID {
		t.Fatalf("the correction did not become current")
	}

	// RE-GATES NOBODY, and this is the assertion the whole design turns on: the
	// Customer who held edition 1 still holds it, and the correction sits above
	// the gating floor rather than moving it, so nothing has to be backfilled.
	var stillHolding string
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT terms_version_id::text FROM customers WHERE email = 'held@example.com'`).Scan(&stillHolding); err != nil {
		t.Fatalf("read the acceptance: %v", err)
	}
	if stillHolding != fresh.Published.VersionID {
		t.Fatalf("the correction moved somebody's acceptance: %q -> %q", fresh.Published.VersionID, stillHolding)
	}
	// The edition that was corrected keeps its bytes: no row an acceptance points
	// at is ever mutated by a publish.
	var correctedHash string
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT content_hash FROM terms_versions WHERE id = $1`, fresh.Published.VersionID).Scan(&correctedHash); err != nil {
		t.Fatalf("the corrected edition went missing: %v", err)
	}
	if correctedHash != fresh.Published.ContentHash {
		t.Fatalf("the corrected edition's fingerprint moved: %q -> %q", fresh.Published.ContentHash, correctedHash)
	}
}

// TestOperatorLegalPublishRefusals walks every refusal the ticket rules, at the
// seam. Each is its own paragraph because each is a different sentence about a
// different mistake — and none of them is enforced only by the screen.
func TestOperatorLegalPublishRefusals(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	fresh := legalPublishWorkspace(t, env, sessionID, operatorLegalPolicyPath)

	// Nothing saved: there is nothing to publish, and a preview promises a look
	// at the text that WILL be published.
	resp, body := publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{"kind": "edition"})
	refusedWith(t, resp, body, http.StatusConflict, "LEGAL_DRAFT_NOT_STORED")

	// Which act this is has no default.
	saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath, draftBody([]string{"en", "es"}, fresh.Draft.Artifacts))
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_PUBLISH_KIND_UNKNOWN")

	// AN INCOMPLETE DRAFT. The Spanish policy is emptied, so a reader would meet
	// a document with a hole in it. Saved happily; refused at publish.
	holed := saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "policy", "es", "")))
	if holed.Publish.Complete || holed.Publish.CanPublish {
		t.Fatalf("a draft with a hole reports complete: %+v", holed.Publish)
	}
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{
		"kind": "edition", "effective_date": holed.Publish.EarliestEffectiveDate})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_PUBLISH_INCOMPLETE")

	// THE TWO REVIEW GATES. A complete draft nobody has looked at is still not
	// publishable, and that is the substitute for a second pair of eyes.
	reworded := saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "policy", "en", "The policy, restated.")))
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{
		"kind": "edition", "effective_date": reworded.Publish.EarliestEffectiveDate})
	refusedWith(t, resp, body, http.StatusConflict, "LEGAL_PUBLISH_NOT_PREVIEWED")

	for _, gap := range reworded.Draft.PreviewGaps {
		resp, previewBody := env.post(t, operatorLegalPolicyPath+"/draft/previews",
			map[string]any{"slug": gap.Slug, "locale": gap.Locale}, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview status=%d error=%+v", resp.StatusCode, previewBody.Error)
		}
	}
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath, map[string]any{
		"kind": "edition", "effective_date": reworded.Publish.EarliestEffectiveDate})
	refusedWith(t, resp, body, http.StatusConflict, "LEGAL_PUBLISH_DIFF_NOT_SEEN")

	// AN EMPTY DIFF. Restore the published words, look at everything, and the two
	// kinds part company: a correction that corrects nothing cannot be recorded,
	// while re-gating over unchanged text is an act an operator may need.
	saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath, draftBody([]string{"en", "es"}, fresh.Draft.Artifacts))
	same := reviewWholeDraft(t, env, sessionID, operatorLegalPolicyPath)
	if !same.Publish.EmptyDiff || !same.Publish.CanPublish || same.Publish.CanCorrect {
		t.Fatalf("an unchanged, fully reviewed draft: %+v", same.Publish)
	}
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath,
		map[string]any{"kind": "correction", "reason": "Nothing at all was wrong with it."})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_CORRECTION_EMPTY_DIFF")

	// A STRUCTURAL CHANGE AS A CORRECTION. An artifact is added: somebody is now
	// being asked for a consent they were not asked for, which is not a typo
	// however small its text.
	withExtra := append(append([]legalArtifactView{}, fresh.Draft.Artifacts...), legalArtifactView{
		Slug:   "label-analytics-consent",
		Bodies: map[string]string{"en": "Analytics, please.", "es": "Analítica, por favor."},
	})
	saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath, draftBody([]string{"en", "es"}, withExtra))
	structural := reviewWholeDraft(t, env, sessionID, operatorLegalPolicyPath)
	if !structural.Publish.Structural || structural.Publish.CanCorrect {
		t.Fatalf("an added artifact: %+v", structural.Publish)
	}
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath,
		map[string]any{"kind": "correction", "reason": "We forgot the analytics box entirely."})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_CORRECTION_STRUCTURAL")

	// A LOCALE-SET CHANGE AS A CORRECTION. English is dropped: that reshapes the
	// hash preimage rather than the fingerprint, so it cannot be a touch-up.
	saveLegalDraftAt(t, env, sessionID, operatorLegalPolicyPath,
		draftBody([]string{"es"}, rewordOne(fresh.Draft.Artifacts, "policy", "es", "El texto, corregido.")))
	dropped := reviewWholeDraft(t, env, sessionID, operatorLegalPolicyPath)
	if !dropped.Publish.LocaleSetChanged || dropped.Publish.CanCorrect {
		t.Fatalf("a dropped language: %+v", dropped.Publish)
	}
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath,
		map[string]any{"kind": "correction", "reason": "The English translation was withdrawn."})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_CORRECTION_LOCALE_SET_CHANGED")

	// AN EFFECTIVE DATE THAT IS NOT ONE.
	resp, body = publishLegal(t, env, sessionID, operatorLegalPolicyPath,
		map[string]any{"kind": "edition", "effective_date": "next Tuesday"})
	refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_EFFECTIVE_DATE_INVALID")
}

// TestOperatorLegalPublishRefusesToDropTheProtectedLocale is its own test
// because it is the strongest guard in the design and the only one that a code
// change and a deploy are needed to lift.
func TestOperatorLegalPublishRefusesToDropTheProtectedLocale(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// BOTH DOCUMENTS, BOTH PUBLISH KINDS. The Policy's Spanish rests on the
	// LOPDP's notice duty and the Terms' on §37 — two footings, two constants,
	// two sentences — and neither may be dropped by either act.
	for _, path := range []string{operatorLegalPolicyPath, operatorLegalTermsPath} {
		fresh := legalPublishWorkspace(t, env, sessionID, path)
		if fresh.Publish.ProtectedLocale != "es" {
			t.Fatalf("%s: protected locale = %q; want es", path, fresh.Publish.ProtectedLocale)
		}

		// English only: the draft would stop publishing Spanish.
		saved := saveLegalDraftAt(t, env, sessionID, path, draftBody([]string{"en"}, fresh.Draft.Artifacts))
		if saved.Publish.ProtectedLocaleKept {
			t.Fatalf("%s: a draft dropping Spanish reports it kept", path)
		}
		reviewed := reviewWholeDraft(t, env, sessionID, path)

		for _, publication := range []map[string]any{
			{"kind": "edition", "effective_date": reviewed.Publish.EarliestEffectiveDate},
			{"kind": "correction", "reason": "The Spanish text is being retired."},
		} {
			resp, body := publishLegal(t, env, sessionID, path, publication)
			refusedWith(t, resp, body, http.StatusBadRequest, "LEGAL_PROTECTED_LOCALE_REQUIRED")
			// The ruled copy, verbatim, with the law's name untranslated and no
			// article number on the Policy's side.
			want := "Spanish cannot be unpublished. The Spanish text is the contract (§37); the English one is a translation of it."
			if path == operatorLegalPolicyPath {
				want = "Spanish cannot be unpublished. The Ley Orgánica de Protección de Datos Personales requires this notice to be given in Spanish."
			}
			if body.Error.Message != want {
				t.Fatalf("%s: refusal = %q; want %q", path, body.Error.Message, want)
			}
		}

		// Put the draft back so the next document starts clean.
		resp, _ := env.deleteJSON(t, path+"/draft", nil, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: discard status=%d", path, resp.StatusCode)
		}
	}
}

// TestOperatorLegalCorrectNowAndPublishTomorrow is the path the ticket rules
// must be available end to end: a typo fixed at once, and a real edition
// scheduled for the night after — so urgency never forces a choice between speed
// and honesty.
func TestOperatorLegalCorrectNowAndPublishTomorrow(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	fresh := legalPublishWorkspace(t, env, sessionID, operatorLegalTermsPath)

	// The typo, corrected now.
	saveLegalDraftAt(t, env, sessionID, operatorLegalTermsPath,
		draftBody([]string{"en", "es"}, rewordOne(fresh.Draft.Artifacts, "terms", "es", "Los términos, con la errata corregida.")))
	reviewWholeDraft(t, env, sessionID, operatorLegalTermsPath)
	corrected := publishLegalOK(t, env, sessionID, operatorLegalTermsPath, map[string]any{
		"kind": "correction", "reason": "A misspelled word in the Spanish body.",
	})
	if corrected.Published.Label != "1.1" {
		t.Fatalf("correction label = %q; want 1.1", corrected.Published.Label)
	}
	// The correction is current at once — the reader sees the fixed text today —
	// and the next gating edition is still generation 2, because a correction is
	// not a generation.
	if corrected.Publish.GatingLabel != "2" {
		t.Fatalf("next gating label = %q; want 2", corrected.Publish.GatingLabel)
	}

	// The real edition, on top of the correction, scheduled.
	saveLegalDraftAt(t, env, sessionID, operatorLegalTermsPath,
		draftBody([]string{"en", "es"}, rewordOne(corrected.Draft.Artifacts, "terms", "en", "The terms, rewritten in earnest.")))
	reviewed := reviewWholeDraft(t, env, sessionID, operatorLegalTermsPath)
	publishLegalOK(t, env, sessionID, operatorLegalTermsPath, map[string]any{
		"kind": "edition", "effective_date": reviewed.Publish.EarliestEffectiveDate,
	})

	var (
		generation, revision int
		effective            time.Time
	)
	if err := env.db.QueryRowContext(t.Context(), `
		SELECT generation, revision, effective_date FROM terms_versions WHERE label = '2'
	`).Scan(&generation, &revision, &effective); err != nil {
		t.Fatalf("read the scheduled edition: %v", err)
	}
	if generation != 2 || revision != 0 {
		t.Fatalf("lineage = %d.%d; want 2.0", generation, revision)
	}
	if effective.Format("2006-01-02") != reviewed.Publish.EarliestEffectiveDate {
		t.Fatalf("effective date = %s; want %s", effective.Format("2006-01-02"), reviewed.Publish.EarliestEffectiveDate)
	}
	// Both rows exist and both are honest: the correction that took effect at
	// once, and the edition that waits for its day. Nothing was mutated to make
	// room for either.
	var editions int
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM terms_versions`).Scan(&editions); err != nil {
		t.Fatalf("count editions: %v", err)
	}
	if editions != 3 {
		t.Fatalf("terms editions = %d; want the seeded 1, the correction 1.1 and the edition 2", editions)
	}
}
