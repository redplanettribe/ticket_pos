import assert from "node:assert/strict";
import test from "node:test";

import {
  canPublishDraft,
  correctionBlocker,
  formatHeadcount,
  localeSetChanged,
  protectedLocaleDropped,
} from "./legal-publish.ts";
import { isStructuralChange, type ArtifactSet } from "./legal-drafts.ts";

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

test("the adulthood declaration's arrival is offered as an edition and refused as a correction", () => {
  // The two modules meet here on purpose: `isStructuralChange` is asked about a
  // real draft — the Terms with the declaration's label inserted between the
  // checkbox label and the body — and its answer is what this module turns into
  // the screen's refusal. THE SLUG IS NOWHERE IN EITHER MODULE'S RULES; it is
  // an artifact the draft gained, which is all the machinery needs to know.
  const published: ArtifactSet = {
    "label-terms-acceptance": { en: "I accept the terms.", es: "Acepto los términos." },
    terms: { en: "# Terms", es: "# Términos" },
  };
  const draft: ArtifactSet = {
    "label-terms-acceptance": published["label-terms-acceptance"],
    "label-adulthood-declaration": {
      en: "I am eighteen years of age or older.",
      es: "Soy mayor de dieciocho años.",
    },
    terms: published.terms,
  };

  const structural = isStructuralChange(published, draft);
  assert.equal(structural, true);

  const state = {
    canPublish: canPublishDraft({ complete: true, previewedAll: true, seenDiff: true }),
    structural,
    localeSetChanged: localeSetChanged(["en", "es"], ["en", "es"]),
    emptyDiff: false,
  };
  // A fully reviewed, complete draft: the gating edition is on offer and the
  // correction is not, and the sentence the operator reads says which fact
  // about the draft withheld it.
  assert.equal(state.canPublish, true);
  assert.equal(correctionBlocker(state), "structural");

  // AND THE SAME IN REVERSE. Dropping the label in a later edition is structural
  // too, so stopping the collection costs a gating publication as well — no
  // ratchet, and no rule anywhere naming this one checkbox.
  const removalState = { ...state, structural: isStructuralChange(draft, published) };
  assert.equal(removalState.structural, true);
  assert.equal(correctionBlocker(removalState), "structural");
});

test("formatHeadcount groups the number the operator is accepting", () => {
  // The exact separator is the locale's business; what must hold is that a
  // thousand people do not read as a bare four digits.
  assert.notEqual(formatHeadcount(1058, "en"), "1058");
  assert.equal(formatHeadcount(1058, "en"), "1,058");
  assert.equal(formatHeadcount(0, "en"), "0");
});
