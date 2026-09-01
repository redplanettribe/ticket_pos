import assert from "node:assert/strict";
import test from "node:test";

import {
  cellStatus,
  completeness,
  diffCells,
  diffHunks,
  draftSlugs,
  hasChanges,
  isStructuralChange,
  toArtifactList,
  toArtifactSet,
  wordDiff,
  type ArtifactSet,
  type DiffOp,
  type DiffToken,
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

// --- wordDiff / diffHunks / diffCells (#562) --------------------------------

/**
 * The diff, tested where it lives.
 *
 * These three are what the operator is shown before publishing is allowed, and
 * #563 will not enable the button until they have been. Rendering them into a
 * component and reading spans back out of the DOM would test React; what needs
 * testing is that a two-word fix reads as two words, that an artifact appearing
 * or disappearing is never buried among comma fixes, and that every one of them
 * answers for text nobody would write.
 */

/** Rejoining every token reproduces each side, which is the diff's own honesty check. */
function reassemble(tokens: DiffToken[], ops: DiffOp[]): string {
  return tokens
    .filter((token) => ops.includes(token.op))
    .map((token) => token.text)
    .join("");
}

test("a two-word fix in a sentence is a two-word diff", () => {
  const tokens = wordDiff("We keep your name for ever.", "We keep your full name for ever.");
  assert.deepEqual(
    tokens.filter((token) => token.op !== "same"),
    [{ op: "added", text: "full " }],
  );
});

test("wordDiff is lossless: the same tokens rebuild both sides exactly", () => {
  const before = "Line one\nLine two  with   odd spacing\n";
  const after = "Line one\nLine three with odd spacing\n";
  const tokens = wordDiff(before, after);
  assert.equal(reassemble(tokens, ["same", "removed"]), before);
  assert.equal(reassemble(tokens, ["same", "added"]), after);
});

test("wordDiff answers for empty text on either side, and for no change at all", () => {
  assert.deepEqual(wordDiff("", ""), []);
  assert.deepEqual(wordDiff("", "new"), [{ op: "added", text: "new" }]);
  assert.deepEqual(wordDiff("gone", ""), [{ op: "removed", text: "gone" }]);
  assert.deepEqual(wordDiff("same words", "same words"), [{ op: "same", text: "same words" }]);
});

test("a moved word is one removal and one addition, not a rewrite of the sentence", () => {
  const tokens = wordDiff("alpha beta gamma", "alpha gamma beta");
  // Whatever the LCS picks, the shared spine must survive: this is the property
  // that separates a subsequence diff from "everything changed".
  const kept = reassemble(tokens, ["same"]);
  assert.ok(kept.includes("alpha"));
  assert.ok(kept.includes("gamma") || kept.includes("beta"));
});

test("a two-word fix in a long document collapses to one hunk, and the rest is counted", () => {
  const lines = Array.from({ length: 280 }, (_, index) => `Clause ${index + 1}.`);
  const before = lines.join("\n");
  const edited = [...lines];
  edited[139] = "Clause 140, as amended.";
  const { hunks, unchangedLines } = diffHunks(before, edited.join("\n"));

  assert.equal(hunks.length, 1);
  // Three lines of context each side, and the changed line itself.
  assert.equal(hunks[0].rows.length, 7);
  assert.equal(hunks[0].rows.filter((row) => row.kind === "changed").length, 1);
  // 139 lines before the hunk, minus the three kept as context.
  assert.equal(hunks[0].skippedBefore, 136);
  assert.equal(unchangedLines, 279);
});

test("the changed line inside a hunk is diffed word by word", () => {
  const { hunks } = diffHunks("one\ntwo\nthree", "one\ntwo point five\nthree");
  const changed = hunks[0].rows.find((row) => row.kind === "changed");
  assert.ok(changed);
  assert.deepEqual(
    changed.tokens.filter((token) => token.op !== "same"),
    [{ op: "added", text: " point five" }],
  );
  assert.equal(changed.beforeLine, 2);
  assert.equal(changed.afterLine, 2);
});

test("an inserted line is an added row, and the line numbers diverge after it", () => {
  const { hunks } = diffHunks("a\nb", "a\ninserted\nb");
  const rows = hunks[0].rows;
  assert.deepEqual(
    rows.map((row) => [row.kind, row.beforeLine, row.afterLine]),
    [
      ["same", 1, 1],
      ["added", null, 2],
      ["same", 2, 3],
    ],
  );
});

test("two distant edits are two hunks, each carrying what it skipped to get there", () => {
  const lines = Array.from({ length: 60 }, (_, index) => `line ${index + 1}`);
  const edited = [...lines];
  edited[4] = "line 5 edited";
  edited[54] = "line 55 edited";
  const { hunks, unchangedLines } = diffHunks(lines.join("\n"), edited.join("\n"));
  assert.equal(hunks.length, 2);
  assert.equal(hunks[0].skippedBefore, 1);
  assert.equal(hunks[1].skippedBefore, 43);
  assert.equal(unchangedLines, 58);
});

test("an unchanged document has no hunks at all, and says how many lines it kept", () => {
  const text = "one\ntwo\nthree";
  assert.deepEqual(diffHunks(text, text), {
    hunks: [],
    unchangedLines: 3,
  });
});

test("an empty side is every line added or every line removed, and never a phantom blank line", () => {
  assert.deepEqual(diffHunks("", "").hunks, []);
  const added = diffHunks("", "one\ntwo");
  assert.deepEqual(
    added.hunks[0].rows.map((row) => row.kind),
    ["added", "added"],
  );
  const removed = diffHunks("one\ntwo", "");
  assert.deepEqual(
    removed.hunks[0].rows.map((row) => row.kind),
    ["removed", "removed"],
  );
});

test("an added artifact sorts before every wording change", () => {
  const draft: ArtifactSet = {
    ...structuredClone(PUBLISHED),
    policy: { en: "# Policy\n\nLonger.", es: "# Política\n\nLarga." },
    "label-analytics-consent": { en: "I agree to analytics.", es: "Acepto analítica." },
  };
  const cells = diffCells(PUBLISHED, draft, ["en", "es"]);
  assert.equal(cells[0].status, "added");
  assert.equal(cells[0].slug, "label-analytics-consent");
  assert.equal(cells[1].status, "added");
  // The reworded policy comes after both halves of the structural change.
  const modified = cells.filter((cell) => cell.status === "modified");
  assert.deepEqual(modified.map((cell) => [cell.slug, cell.locale]), [["policy", "en"]]);
});

test("a removed artifact sorts first too, carrying the text that is going away", () => {
  const draft = structuredClone(PUBLISHED);
  delete draft["label-policy-acceptance"];
  const cells = diffCells(PUBLISHED, draft, ["en", "es"]);
  assert.deepEqual(
    cells.slice(0, 2).map((cell) => [cell.slug, cell.locale, cell.status]),
    [
      ["label-policy-acceptance", "en", "removed"],
      ["label-policy-acceptance", "es", "removed"],
    ],
  );
  assert.equal(cells[0].before, "I accept the policy.");
  assert.equal(cells[0].after, "");
});

test("unchanged cells are returned, last, so the count on screen is the list's own length", () => {
  const draft = reworded();
  const cells = diffCells(PUBLISHED, draft, ["en", "es"]);
  assert.equal(cells.length, 6);
  const unchanged = cells.filter((cell) => cell.status === "unchanged");
  assert.equal(unchanged.length, 5);
  // Every one of them is at the end of the list.
  assert.deepEqual(cells.slice(1), unchanged);
});

test("the diff's unit is the cell: Spanish is untangled from English", () => {
  const draft = reworded();
  const cells = diffCells(PUBLISHED, draft, ["en", "es"]);
  const modified = cells.filter((cell) => cell.status === "modified");
  assert.deepEqual(modified.map((cell) => [cell.slug, cell.locale]), [["short-notice", "en"]]);
});
