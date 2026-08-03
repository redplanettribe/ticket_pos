import type { PublicTag } from "@/lib/api";

/**
 * What a Tag is called on the page the reader is on.
 *
 * A Preset Tag is the system's own word for a kind of Event, so it is rendered
 * in the page's Locale: the twelve of them are seeded by a migration and their
 * copy is written in messages/{en,es}.json under `tags`, keyed on canonical
 * key (ADR 0027). A Custom Tag is an Organization's own word — "Techno",
 * "Cumbia", the name of a scene — and is rendered exactly as it was typed, in
 * every Locale, because nobody owes it a translation and nobody could write
 * one at the moment it is coined.
 *
 * So a Spanish card can read "Música" beside "Techno". That is the intended
 * result rather than a gap in the catalog: the first is a category this product
 * offers, the second is a name.
 *
 * Keying on `canonical_key` rather than on `name` is what lets the copy be a
 * copy edit. `name` is the English display name, which the API is free to
 * restyle — and which, once the chip in front of the reader says "Artes y
 * teatro", is no longer recoverable from what they are looking at.
 */

/**
 * The subset of next-intl's translator this needs, and the seam where the
 * catalog's typed keys are given up.
 *
 * `useTranslations("tags")` is typed to the twelve keys en.json holds, which is
 * exactly the guarantee that cannot survive here: the key arrives from the API
 * at runtime, and the whole point of the fallback below is to behave when it is
 * one the catalog has never heard of. The looser type is confined to this file
 * so no call site has to write the cast itself.
 */
export type TagTranslator = {
  (key: string): string;
  has(key: string): boolean;
};

export function tagName(tag: PublicTag, t: TagTranslator): string {
  // A Custom Tag is never looked up: an entry under its key would mean copy
  // that contradicts "rendered as typed", and the day it were promoted to a
  // Preset Tag the rule would quietly stop being true.
  if (!tag.curated) return tag.name;

  // A Preset Tag the catalog does not know degrades to the API's English,
  // never to a blank and never to the lowercase key. This is not a
  // theoretical branch: ADR 0004 makes promoting a Custom Tag into the chip
  // bar "a one-flag change", so a Preset Tag can start existing in production
  // without a commit that could have carried its Spanish.
  return t.has(tag.canonical_key) ? t(tag.canonical_key) : tag.name;
}
