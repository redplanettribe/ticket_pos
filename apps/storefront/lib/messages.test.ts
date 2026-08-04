import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { LOCALES } from "./locale.ts";

/**
 * The catalogs are held to the same shape, because nothing else is holding
 * them.
 *
 * en.json is typed — global.d.ts makes every key part of `AppConfig`, so a call
 * site that asks for a key it does not have fails to compile. es.json is typed
 * by nothing at all: it is loaded by filename at request time (i18n/request.ts),
 * and a key missing from it is not a build error, it is a Spanish page with a
 * hole in it that only a Spanish-reading visitor ever sees.
 *
 * So the compiler guards English and this file guards the rest. It asserts the
 * two things a translation can get wrong on its own — a key that exists in one
 * catalog only, and a value that is present but empty — and it reports the
 * offending paths rather than a boolean, because "es.json does not match" is
 * not something anyone can act on at 6pm.
 *
 * It deliberately does NOT assert that the Spanish differs from the English. A
 * message that is only placeholders and punctuation — "{count} × {ticketType}",
 * "{event} · {organization}" — is correctly identical in both catalogs, so an
 * assertion like that would fail on the translations that are right.
 */

type Catalog = { [key: string]: string | Catalog };

function load(locale: string): Catalog {
  return JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  ) as Catalog;
}

/**
 * Every message in a catalog as "namespace.key", nesting and all.
 *
 * Flattening is what makes a namespace that turned into a string (or the other
 * way round) show up as an ordinary key mismatch instead of a silently
 * mistyped comparison.
 */
function flatten(catalog: Catalog, prefix = ""): Map<string, string> {
  const messages = new Map<string, string>();
  for (const [key, value] of Object.entries(catalog)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") {
      messages.set(path, value);
    } else {
      for (const [nested, message] of flatten(value, path)) {
        messages.set(nested, message);
      }
    }
  }
  return messages;
}

const catalogs = new Map(LOCALES.map((locale) => [locale, flatten(load(locale))]));
const [reference, ...others] = LOCALES;
const referenceMessages = catalogs.get(reference)!;

test("every catalog holds exactly the keys en.json holds", () => {
  // Every offending path is collected before anything is asserted, so one run
  // names all of them. Asserting per locale as they are found would report the
  // first missing key and hide the other nine behind it.
  const problems: string[] = [];
  for (const locale of others) {
    const messages = catalogs.get(locale)!;
    for (const key of referenceMessages.keys()) {
      if (!messages.has(key)) problems.push(`${locale}.json is missing ${key}`);
    }
    for (const key of messages.keys()) {
      // A key only the translation has is copy nothing renders: en.json is the
      // typed source of truth, so no call site can ask for it.
      if (!referenceMessages.has(key)) problems.push(`${locale}.json has a stray ${key}`);
    }
  }

  assert.deepEqual(problems, [], `\n${problems.join("\n")}\n`);
});

/**
 * The Preset Tags seeded by migration — the twelve of
 * backend/migrations/009_event_tags.sql plus Technology (047) — by canonical
 * key.
 *
 * Copied rather than read, because this app cannot see a Go migration — and
 * that copy is the point of the test below rather than a weakness in it. The
 * hazard is not that the seed changes silently; it is that the Preset tier
 * grows the way ADR 0004 intends it to, by flipping `curated` on a Custom Tag
 * in a single UPDATE, with no code change anywhere to hang a review on. A
 * Preset Tag added that way reaches the chip bar in English and nothing
 * complains.
 *
 * This list cannot catch that either. What it catches is the other half: a seed
 * migration that adds a Preset Tag and forgets the copy, where updating this
 * fixture is the obvious next keystroke and the failure names the missing key.
 * The Locale fallback in lib/tag-name.ts covers the UPDATE-in-production case,
 * which is why that fallback is not optional.
 */
const SEEDED_PRESET_TAG_KEYS = [
  "music",
  "nightlife",
  "arts & theatre",
  "comedy",
  "food & drink",
  "festival",
  "sports",
  "workshop",
  "conference",
  "community",
  "family",
  "film",
  "technology",
];

test("every seeded Preset Tag has copy in every catalog", () => {
  const problems: string[] = [];
  for (const locale of LOCALES) {
    const messages = catalogs.get(locale)!;
    for (const key of SEEDED_PRESET_TAG_KEYS) {
      // No canonical key can contain a ".", so the flattened path is
      // unambiguous: the tag charset is letters, digits, spaces and hyphens
      // (backend/internal/catalog/service/tags.go), and the two seeds carrying
      // an "&" predate it by being raw SQL.
      if (!messages.has(`tags.${key}`)) {
        problems.push(`${locale}.json has no copy for the Preset Tag ${key}`);
      }
    }
  }

  assert.deepEqual(
    problems,
    [],
    `\n${problems.join("\n")}\nA Preset Tag with no copy renders in English (ADR 0027).\n`,
  );
});

test("no message is empty", () => {
  const problems: string[] = [];
  for (const locale of LOCALES) {
    for (const [key, message] of catalogs.get(locale)!) {
      // Trimmed, because a value of " " is a blank label with extra steps.
      if (message.trim() === "") problems.push(`${locale}.json is empty at ${key}`);
    }
  }

  assert.deepEqual(
    problems,
    [],
    `\n${problems.join("\n")}\nAn untranslated message is the English one, never "".\n`,
  );
});
