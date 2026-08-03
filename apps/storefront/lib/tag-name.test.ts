import assert from "node:assert/strict";
import test from "node:test";

import { tagName, type TagTranslator } from "./tag-name.ts";

/**
 * A stand-in for the `tags` namespace, holding only what a catalog would.
 *
 * The real translator is next-intl's, which cannot be built without a request
 * — and would not test anything more than this does. What is worth pinning is
 * the branching, and in particular that a key the catalog is missing takes the
 * fallback rather than throwing or rendering the key.
 */
function translator(messages: Record<string, string>): TagTranslator {
  const t = ((key: string) => {
    const message = messages[key];
    if (message === undefined) throw new Error(`no message for ${key}`);
    return message;
  }) as TagTranslator;
  t.has = (key: string) => key in messages;
  return t;
}

const es = translator({ "arts & theatre": "Arte y teatro", music: "Música" });

test("a Preset Tag is worded in the page's Locale", () => {
  const name = tagName(
    { canonical_key: "arts & theatre", name: "Arts & Theatre", curated: true },
    es,
  );

  assert.equal(name, "Arte y teatro");
});

test("a Custom Tag is rendered as it was typed", () => {
  const name = tagName({ canonical_key: "techno", name: "Techno", curated: false }, es);

  assert.equal(name, "Techno");
});

test("a Custom Tag is rendered as typed even where the catalog has its key", () => {
  // Guards the rule rather than the code path: "Custom Tags render as typed"
  // has to hold on the day someone adds copy under a Custom Tag's key, not
  // only while nobody has. The lookup order is what makes that true.
  const name = tagName({ canonical_key: "music", name: "music scene", curated: false }, es);

  assert.equal(name, "music scene");
});

test("a Preset Tag the catalog has never heard of degrades to English", () => {
  // ADR 0004 grows the Preset tier by flipping `curated` in a single UPDATE, so
  // this Tag can reach the chip bar in production without a commit that could
  // have carried its Spanish. It must read as English, never as a blank chip
  // and never as the lowercase key.
  const name = tagName({ canonical_key: "cumbia", name: "Cumbia", curated: true }, es);

  assert.equal(name, "Cumbia");
});
