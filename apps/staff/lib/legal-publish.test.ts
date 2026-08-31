import assert from "node:assert/strict";
import test from "node:test";

import {
  canPublishDraft,
  correctionBlocker,
  formatHeadcount,
  localeSetChanged,
  protectedLocaleDropped,
} from "./legal-publish.ts";

/**
 * The publish step's rules (#563), tested directly.
 *
 * The screen these drive puts one irreversible act behind three gates and hides
 * a second, quieter act beneath it. What is worth pinning is not that the
 * buttons render but that the rules say the right thing about a draft — so these
 * assertions are about the rules, and the component is not tested in their
 * place.
 */

test("canPublishDraft needs all three gates and not two", () => {
  assert.equal(canPublishDraft({ complete: true, previewedAll: true, seenDiff: true }), true);
  // Each one alone is enough to withhold the button. A complete draft nobody has
  // read is exactly the failure the review gates exist for.
  assert.equal(canPublishDraft({ complete: false, previewedAll: true, seenDiff: true }), false);
  assert.equal(canPublishDraft({ complete: true, previewedAll: false, seenDiff: true }), false);
  assert.equal(canPublishDraft({ complete: true, previewedAll: true, seenDiff: false }), false);
});

test("correctionBlocker reports the most specific reason", () => {
  const clear = {
    canPublish: true,
    structural: false,
    localeSetChanged: false,
    emptyDiff: false,
  };
  assert.equal(correctionBlocker(clear), null);

  // A structural change outranks everything, including the gates: it is the one
  // fact the operator has to decide something about, and no amount of previewing
  // will make an added artifact a typo.
  assert.equal(
    correctionBlocker({ ...clear, canPublish: false, structural: true, emptyDiff: true }),
    "structural",
  );
  assert.equal(correctionBlocker({ ...clear, localeSetChanged: true }), "locales");
  assert.equal(correctionBlocker({ ...clear, emptyDiff: true }), "empty");
  assert.equal(correctionBlocker({ ...clear, canPublish: false }), "gates");
});

test("localeSetChanged is about the set and not the list", () => {
  assert.equal(localeSetChanged(["en", "es"], ["es", "en"]), false);
  assert.equal(localeSetChanged(["en", "es"], ["en", "es"]), false);
  assert.equal(localeSetChanged(["en", "es"], ["es"]), true);
  assert.equal(localeSetChanged(["es"], ["en", "es"]), true);
});

test("protectedLocaleDropped notices the one language that cannot go", () => {
  assert.equal(protectedLocaleDropped("es", ["en", "es"]), false);
  assert.equal(protectedLocaleDropped("es", ["en"]), true);
  assert.equal(protectedLocaleDropped("es", []), true);
});

test("formatHeadcount groups the number the operator is accepting", () => {
  // The exact separator is the locale's business; what must hold is that a
  // thousand people do not read as a bare four digits.
  assert.notEqual(formatHeadcount(1058, "en"), "1058");
  assert.equal(formatHeadcount(1058, "en"), "1,058");
  assert.equal(formatHeadcount(0, "en"), "0");
});
