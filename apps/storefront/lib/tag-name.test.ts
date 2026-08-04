import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { createTranslator } from "next-intl";

import { tagName, toTagTranslator } from "./tag-name.ts";

/**
 * next-intl's own translator, over the real es.json.
 *
 * Built rather than stubbed, and over the shipped catalog rather than a fixture,
 * because two of the things worth pinning here are properties of next-intl and
 * of the file — that a key holding spaces and an "&" resolves at all (next-intl
 * splits key paths on ".", and "arts & theatre" survives only because no
 * canonical key can contain one), and that the Spanish a reader sees is the
 * Spanish that shipped. A hand-written map would assert neither.
 */
function catalog(locale: string) {
  const messages = JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  );
  return toTagTranslator(createTranslator({ locale, messages, namespace: "tags" }));
}

const es = catalog("es");

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
