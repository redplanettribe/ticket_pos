package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Legal Center's review state (#562, spec #556): what the operator has
// LOOKED AT, which #563 turns into publish preconditions.
//
// What this slice must prove is the thing a flag would get wrong. Recording
// "previewed" and "diff seen" is easy; recording them so that they EXPIRE WHEN
// THE WORDS MOVE is the whole point, because otherwise an operator previews a
// paragraph, rewrites it, and the publish button stays lit over text nobody has
// ever seen rendered. So the assertions here are all of the form "look at it,
// change it, and watch the looking stop counting" — and the mirror of that:
// saving a draft that changed nothing must NOT un-preview an afternoon's
// reading, or Save would be a thing operators learn to avoid.

// legalReviewView is the review half of the draft. It is decoded through its own
// struct rather than added to operator_legal_center_test.go's legalDraftView so
// that the two slices' assertions stay independent.
type legalReviewView struct {
	Stored           bool     `json:"stored"`
	PublishedLocales []string `json:"published_locales"`
	Artifacts        []struct {
		Slug   string            `json:"slug"`
		Bodies map[string]string `json:"bodies"`
	} `json:"artifacts"`
	Previewed []struct {
		Slug        string `json:"slug"`
		Locale      string `json:"locale"`
		PreviewedBy string `json:"previewed_by"`
		PreviewedAt string `json:"previewed_at"`
	} `json:"previewed"`
	PreviewGaps []struct {
		Slug   string `json:"slug"`
		Locale string `json:"locale"`
	} `json:"preview_gaps"`
	PreviewedAll bool   `json:"previewed_all"`
	SeenDiff     bool   `json:"seen_diff"`
	DiffSeenBy   string `json:"diff_seen_by"`
}

type legalReviewWorkspaceView struct {
	Published struct {
		VersionID string `json:"version_id"`
	} `json:"published"`
	Draft legalReviewView `json:"draft"`
}

func legalReviewWorkspace(t *testing.T, env *testEnv, sessionID string) legalReviewWorkspaceView {
	t.Helper()
	var workspace legalReviewWorkspaceView
	operatorGetOK(t, env, sessionID, operatorLegalPolicyPath, &workspace)
	return workspace
}

func previewLegalCell(t *testing.T, env *testEnv, sessionID, slug, locale string) legalReviewWorkspaceView {
	t.Helper()
	resp, body := env.post(t, operatorLegalPolicyPath+"/draft/previews",
		map[string]any{"slug": slug, "locale": locale}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview %s/%s status=%d error=%+v", slug, locale, resp.StatusCode, body.Error)
	}
	var workspace legalReviewWorkspaceView
	if err := json.Unmarshal(body.Data, &workspace); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return workspace
}

// TestOperatorLegalReviewExpiresWhenTheWordsMove walks one draft through being
// looked at, edited, and looked at again.
func TestOperatorLegalReviewExpiresWhenTheWordsMove(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// A saved draft, identical to the published edition to begin with.
	fresh := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	artifacts := append([]legalArtifactView{}, fresh.Draft.Artifacts...)
	saved := saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, artifacts))
	if !saved.Draft.Stored {
		t.Fatalf("the draft did not save")
	}

	// Nothing has been looked at yet, and the gaps name every cell that will have
	// to be — one per artifact per published language.
	before := legalReviewWorkspace(t, env, sessionID)
	if before.Draft.PreviewedAll || before.Draft.SeenDiff {
		t.Fatalf("a draft nobody has looked at reports itself reviewed: %+v", before.Draft)
	}
	cells := 0
	for _, artifact := range before.Draft.Artifacts {
		cells += len(artifact.Bodies)
	}
	if len(before.Draft.PreviewGaps) != cells {
		t.Fatalf("preview gaps = %d; want one per written cell (%d)", len(before.Draft.PreviewGaps), cells)
	}

	// Preview every cell. The last one flips previewed_all, and not before.
	var latest legalReviewWorkspaceView
	for index, gap := range before.Draft.PreviewGaps {
		latest = previewLegalCell(t, env, sessionID, gap.Slug, gap.Locale)
		wantAll := index == len(before.Draft.PreviewGaps)-1
		if latest.Draft.PreviewedAll != wantAll {
			t.Fatalf("after %d of %d previews previewed_all=%v; want %v",
				index+1, len(before.Draft.PreviewGaps), latest.Draft.PreviewedAll, wantAll)
		}
	}
	if len(latest.Draft.Previewed) != cells || latest.Draft.Previewed[0].PreviewedBy != "operator@example.com" {
		t.Fatalf("previewed = %+v; want every cell, attributed to the session", latest.Draft.Previewed)
	}

	// Previewing the same cell twice is ONE fact, not two.
	again := previewLegalCell(t, env, sessionID, before.Draft.PreviewGaps[0].Slug, before.Draft.PreviewGaps[0].Locale)
	if len(again.Draft.Previewed) != cells {
		t.Fatalf("previewing twice recorded %d cells; want %d", len(again.Draft.Previewed), cells)
	}

	// The diff, seen.
	resp, body := env.post(t, operatorLegalPolicyPath+"/draft/diff-seen", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff-seen status=%d error=%+v", resp.StatusCode, body.Error)
	}
	seen := legalReviewWorkspace(t, env, sessionID)
	if !seen.Draft.SeenDiff || seen.Draft.DiffSeenBy != "operator@example.com" {
		t.Fatalf("diff not recorded as seen: %+v", seen.Draft)
	}

	// SAVING WITHOUT CHANGING ANYTHING KEEPS ALL OF IT. Pressing Save twice must
	// not undo an afternoon's reading.
	resaved := saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, artifacts))
	_ = resaved
	unchanged := legalReviewWorkspace(t, env, sessionID)
	if !unchanged.Draft.PreviewedAll || !unchanged.Draft.SeenDiff {
		t.Fatalf("a save that changed nothing threw the review away: %+v", unchanged.Draft)
	}

	// NOW EDIT ONE CELL. That cell's preview lapses — and only that cell's — and
	// the diff lapses with it, because it is no longer a diff of this draft.
	edited := make([]legalArtifactView, len(artifacts))
	copy(edited, artifacts)
	target := edited[0]
	bodies := map[string]string{}
	for locale, text := range target.Bodies {
		bodies[locale] = text
	}
	bodies["en"] = "Rewritten after the preview."
	edited[0] = legalArtifactView{Slug: target.Slug, Bodies: bodies}
	saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, edited))

	after := legalReviewWorkspace(t, env, sessionID)
	if after.Draft.PreviewedAll {
		t.Fatalf("a rewritten cell is still reported as previewed")
	}
	if after.Draft.SeenDiff {
		t.Fatalf("the diff still counts as seen after the draft moved under it")
	}
	if len(after.Draft.PreviewGaps) != 1 {
		t.Fatalf("preview gaps = %+v; want only the rewritten cell", after.Draft.PreviewGaps)
	}
	if after.Draft.PreviewGaps[0].Slug != target.Slug || after.Draft.PreviewGaps[0].Locale != "en" {
		t.Fatalf("preview gap = %+v; want the cell that was rewritten", after.Draft.PreviewGaps[0])
	}
	if len(after.Draft.Previewed) != cells-1 {
		t.Fatalf("previewed = %d cells; want the other %d to survive an unrelated edit",
			len(after.Draft.Previewed), cells-1)
	}

	// Looking at it again is how it comes back.
	back := previewLegalCell(t, env, sessionID, target.Slug, "en")
	if !back.Draft.PreviewedAll {
		t.Fatalf("previewing the rewritten cell did not restore previewed_all: %+v", back.Draft)
	}
}

// TestOperatorLegalReviewRefusesWhatCannotBeLookedAt: the two refusals, and the
// fact that discarding a draft discards what was reviewed about it.
func TestOperatorLegalReviewRefusesWhatCannotBeLookedAt(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// No draft: 409 on both calls. A preview promises a look at the text that
	// WILL be published, and unsaved text will not be.
	resp, body := env.post(t, operatorLegalPolicyPath+"/draft/previews",
		map[string]any{"slug": "policy", "locale": "en"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "LEGAL_DRAFT_NOT_STORED" {
		t.Fatalf("preview with no draft: status=%d error=%+v; want 409 LEGAL_DRAFT_NOT_STORED", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, operatorLegalPolicyPath+"/draft/diff-seen", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "LEGAL_DRAFT_NOT_STORED" {
		t.Fatalf("diff-seen with no draft: status=%d error=%+v; want 409 LEGAL_DRAFT_NOT_STORED", resp.StatusCode, body.Error)
	}

	fresh := legalWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	artifacts := append([]legalArtifactView{}, fresh.Draft.Artifacts...)
	// An artifact whose Spanish nobody has written: a hole, and holes are not
	// previewable. The completeness rule refuses them at publish (#563).
	artifacts = append(artifacts, legalArtifactView{
		Slug:   "label-analytics-consent",
		Bodies: map[string]string{"en": "I agree to analytics."},
	})
	saveLegalDraft(t, env, sessionID, draftBody([]string{"en", "es"}, artifacts))

	resp, body = env.post(t, operatorLegalPolicyPath+"/draft/previews",
		map[string]any{"slug": "label-analytics-consent", "locale": "es"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_DRAFT_CELL_NOT_FOUND" {
		t.Fatalf("preview of an empty cell: status=%d error=%+v; want 400 LEGAL_DRAFT_CELL_NOT_FOUND", resp.StatusCode, body.Error)
	}

	// A language this platform does not publish in is refused on the way in,
	// exactly as it is when saving.
	resp, body = env.post(t, operatorLegalPolicyPath+"/draft/previews",
		map[string]any{"slug": "policy", "locale": "fr"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "LEGAL_DRAFT_LOCALE_UNSUPPORTED" {
		t.Fatalf("preview in an unsupported language: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Discarding the draft takes the review with it: there is nothing left to
	// have looked at.
	previewLegalCell(t, env, sessionID, "policy", "en")
	if resp, body := env.deleteJSON(t, operatorLegalPolicyPath+"/draft", nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("discard status=%d error=%+v", resp.StatusCode, body.Error)
	}
	after := legalReviewWorkspace(t, env, sessionID)
	if after.Draft.Stored || len(after.Draft.Previewed) != 0 || after.Draft.SeenDiff {
		t.Fatalf("a discarded draft kept its review state: %+v", after.Draft)
	}
}
