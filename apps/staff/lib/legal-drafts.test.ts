import assert from "node:assert/strict";
import test from "node:test";

import {
  cellStatus,
  completeness,
  draftSlugs,
  hasChanges,
  isStructuralChange,
  toArtifactList,
  toArtifactSet,
  type ArtifactSet,
} from "./legal-drafts.ts";

/**
 * The Legal Center's rules, tested where they live (#561).
 *
 * These four functions decide whether a legal document may be published — every
 * artifact in every published language, and a change to the artifact SET is
 * structural and therefore not a correction. Testing them through the editor
 * would be testing them through three layers of DOM: a textarea's onChange, a
 * React state update and a coloured dot. So the component is not tested here or
 * anywhere, and these are.
 */

const PUBLISHED: ArtifactSet = {
  "short-notice": { en: "We keep your name.", es: "Guardamos tu nombre." },
  "label-policy-acceptance": { en: "I accept the policy.", es: "Acepto la política." },
  policy: { en: "# Policy\n\nLong.", es: "# Política\n\nLarga." },
};

/** A draft that changed one word in English and nothing else. */
function reworded(): ArtifactSet {
  return {
    ...structuredClone(PUBLISHED),
    "short-notice": { en: "We keep your full name.", es: "Guardamos tu nombre." },
  };
}

// --- cellStatus ------------------------------------------------------------

test("an untouched cell is unchanged, and whitespace alone is not an edit", () => {
  assert.equal(cellStatus(PUBLISHED, structuredClone(PUBLISHED), "policy", "en"), "unchanged");
  const padded: ArtifactSet = { ...PUBLISHED, policy: { en: "  # Policy\n\nLong.  ", es: "x" } };
  assert.equal(cellStatus(PUBLISHED, padded, "policy", "en"), "unchanged");
});

test("a reworded cell is modified, in that language alone", () => {
  const draft = reworded();
  assert.equal(cellStatus(PUBLISHED, draft, "short-notice", "en"), "modified");
  assert.equal(cellStatus(PUBLISHED, draft, "short-notice", "es"), "unchanged");
});

test("an artifact the draft invents is added where it has text and missing where it has none", () => {
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    "label-analytics-consent": { en: "I agree to analytics." },
  };
  assert.equal(cellStatus(PUBLISHED, draft, "label-analytics-consent", "en"), "added");
  // The Spanish has not been written: this is the half-translated language the
  // completeness rule exists to catch, not a language that was dropped.
  assert.equal(cellStatus(PUBLISHED, draft, "label-analytics-consent", "es"), "missing");
});

test("an artifact the draft drops is removed, and an emptied cell is missing rather than removed", () => {
  const dropped: ArtifactSet = structuredClone(PUBLISHED);
  delete dropped["label-policy-acceptance"];
  assert.equal(cellStatus(PUBLISHED, dropped, "label-policy-acceptance", "en"), "removed");

  // Cleared in the textarea rather than deleted: the same fact about the
  // document, and the API stores both as an absent row — so it must not read as
  // a structural removal on one path and a hole on the other.
  const emptied: ArtifactSet = { ...structuredClone(PUBLISHED), "label-policy-acceptance": { en: "   ", es: "Acepto." } };
  assert.equal(cellStatus(PUBLISHED, emptied, "label-policy-acceptance", "en"), "missing");
});

test("a slug neither side has is missing, not a crash", () => {
  assert.equal(cellStatus(PUBLISHED, PUBLISHED, "no-such-slug", "en"), "missing");
});

// --- completeness ----------------------------------------------------------

test("a draft written in both languages is complete", () => {
  const result = completeness(PUBLISHED, reworded());
  assert.equal(result.complete, true);
  assert.deepEqual(result.gaps, []);
});

test("a half-translated artifact is an incomplete draft, and the gap names the cell", () => {
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    "label-analytics-consent": { en: "I agree to analytics." },
  };
  const result = completeness(PUBLISHED, draft);
  assert.equal(result.complete, false);
  assert.deepEqual(result.gaps, [{ slug: "label-analytics-consent", locale: "es" }]);
});

test("a language the draft does not publish is not a gap", () => {
  // Dropping Spanish is the ONLY direction exercisable today, and it must make
  // the draft complete rather than leaving it permanently short: the languages
  // are the draft's explicit set, never inferred from which cells are filled.
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    "label-analytics-consent": { en: "I agree to analytics." },
  };
  assert.deepEqual(completeness(PUBLISHED, draft, ["en"]), { complete: true, gaps: [] });
});

test("completeness asks about the DRAFT's artifacts, so a dropped one leaves no gap behind", () => {
  const dropped: ArtifactSet = structuredClone(PUBLISHED);
  delete dropped["label-policy-acceptance"];
  assert.deepEqual(completeness(PUBLISHED, dropped).gaps, []);
});

// --- isStructuralChange ----------------------------------------------------

test("rewording every cell in both languages is not structural", () => {
  const draft: ArtifactSet = {
    "short-notice": { en: "a", es: "b" },
    "label-policy-acceptance": { en: "c", es: "d" },
    policy: { en: "e", es: "f" },
  };
  assert.equal(isStructuralChange(PUBLISHED, draft), false);
});

test("adding an artifact is structural", () => {
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    "label-analytics-consent": { en: "I agree.", es: "Acepto." },
  };
  assert.equal(isStructuralChange(PUBLISHED, draft), true);
});

test("removing an artifact is structural", () => {
  const draft: ArtifactSet = structuredClone(PUBLISHED);
  delete draft["label-marketing-consent"];
  // It was not published either, so removing what was never there is not a
  // change at all — the fixture only publishes three artifacts.
  assert.equal(isStructuralChange(PUBLISHED, draft), false);

  const real: ArtifactSet = structuredClone(PUBLISHED);
  delete real.policy;
  assert.equal(isStructuralChange(PUBLISHED, real), true);
});

test("an artifact half-written is NOT yet structural, because nothing has been added", () => {
  // A brand-new slug with every cell still blank is a row the operator has just
  // created and not yet filled. It must not flip the publish path to
  // "structural" — and it must not read as complete either.
  const draft: ArtifactSet = { ...structuredClone(PUBLISHED), "label-analytics-consent": { en: "  " } };
  assert.equal(isStructuralChange(PUBLISHED, draft), false);
  assert.equal(completeness(PUBLISHED, draft).complete, false);
});

test("a language the draft does not publish cannot make a change structural", () => {
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    "label-analytics-consent": { en: "I agree." },
  };
  assert.equal(isStructuralChange(PUBLISHED, draft, ["es"]), false);
  assert.equal(isStructuralChange(PUBLISHED, draft, ["en"]), true);
});

// --- draftSlugs ------------------------------------------------------------

test("the draft's own key order is the ordinal order", () => {
  const draft: ArtifactSet = {
    "short-notice": { en: "a" },
    "label-analytics-consent": { en: "b" },
    policy: { en: "c" },
  };
  assert.deepEqual(
    draftSlugs(draft).map((spec) => [spec.slug, spec.ordinal]),
    [
      ["short-notice", 1],
      ["label-analytics-consent", 2],
      ["policy", 3],
    ],
  );
});

test("a one-line label and a full document get different sizes; an invented slug is unknown", () => {
  const specs = draftSlugs({
    "label-policy-acceptance": { en: "a" },
    policy: { en: "b" },
    "house-rules": { en: "c" },
  });
  assert.deepEqual(
    specs.map((spec) => [spec.size, spec.known]),
    [
      ["line", true],
      ["document", true],
      // Nothing renders `house-rules`. The big box is the safe guess: a label in
      // a tall box is untidy, a page of prose in a one-line box is unusable.
      ["document", false],
    ],
  );
});

test("draftSlugs answers for an empty draft", () => {
  assert.deepEqual(draftSlugs({}), []);
  assert.deepEqual(completeness(PUBLISHED, {}), { complete: true, gaps: [] });
});

// --- hasChanges ------------------------------------------------------------

test("a draft equal to the published edition has no changes, and a reworded one does", () => {
  assert.equal(hasChanges(PUBLISHED, structuredClone(PUBLISHED)), false);
  assert.equal(hasChanges(PUBLISHED, reworded()), true);
});

// --- the API's shape, in and out -------------------------------------------

test("the API's ordinal-ordered list becomes an ArtifactSet whose key order is that order", () => {
  const set = toArtifactSet([
    { slug: "short-notice", ordinal: 1, bodies: { en: "a", es: "b" } },
    { slug: "policy", ordinal: 2, bodies: { en: "c" } },
  ]);
  assert.deepEqual(Object.keys(set), ["short-notice", "policy"]);
  assert.deepEqual(set.policy, { en: "c" });
});

test("the list going back carries no ordinals, so nothing can disagree with the order", () => {
  const list = toArtifactList({ "short-notice": { en: "a" }, policy: { en: "c", es: "d" } });
  assert.deepEqual(list, [
    { slug: "short-notice", bodies: { en: "a" } },
    { slug: "policy", bodies: { en: "c", es: "d" } },
  ]);
  assert.equal(Object.hasOwn(list[0], "ordinal"), false);
});
