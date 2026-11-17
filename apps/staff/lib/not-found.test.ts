import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

/**
 * The not-found screen's words (#799), as each catalog holds them. Which catalog
 * a screen reads is next-intl's, from the locale the root layout settled; what
 * is held here is that each locale has its own words for the screen, and only
 * the words a not-found screen needs: no retry and no reference code, because
 * nothing failed.
 */

type NotFoundCopy = Record<string, string>;

function notFoundCopy(locale: "en" | "es"): NotFoundCopy {
  const catalog = JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as {
    notFound: NotFoundCopy;
  };
  return catalog.notFound;
}

test("each locale has the not-found screen's title, description and way back, and nothing to retry or quote", () => {
  for (const locale of ["en", "es"] as const) {
    assert.deepEqual(Object.keys(notFoundCopy(locale)).sort(), ["back", "description", "title"], locale);
  }
});

test("the Spanish screen speaks Spanish and the English screen English", () => {
  const en = notFoundCopy("en");
  const es = notFoundCopy("es");
  for (const key of ["title", "description", "back"]) {
    assert.notEqual(es[key], en[key], `notFound.${key} is the same in both catalogs`);
  }
  assert.match(es.title, /página/i);
  assert.match(en.title, /page/i);
});

test("the screen never admits that a concealed page exists", () => {
  // Operator pages call notFound() to hide themselves from everyone else, so the
  // words must not hint at a page that is forbidden, removed or merely hidden.
  for (const locale of ["en", "es"] as const) {
    for (const message of Object.values(notFoundCopy(locale))) {
      assert.doesNotMatch(message, /permis|access|acceso|forbidden|prohib|remov|delet|elimin|hidden|ocult/i, locale);
    }
  }
});
