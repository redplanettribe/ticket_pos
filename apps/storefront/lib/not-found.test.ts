import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { LOCALES, type AppLocale } from "./locale.ts";
import { REQUESTED_PATH_HEADER, notFoundLocale } from "./not-found.ts";

type NotFoundCopy = { title: string; description: string; back: string };

/** The not-found screen's words, as a locale's catalog holds them. */
function notFoundCopy(locale: AppLocale): NotFoundCopy {
  const catalog = JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as {
    notFound: NotFoundCopy;
  };
  return catalog.notFound;
}

function requestFor(path: string | null) {
  return new Headers(path === null ? {} : { [REQUESTED_PATH_HEADER]: path });
}

test("an address no route matches gets the screen in the language the address names", () => {
  assert.equal(notFoundLocale(requestFor("/es/a/b/c/d")), "es");
  assert.equal(notFoundLocale(requestFor("/en/a/b/c/d")), "en");
  assert.equal(notFoundLocale(requestFor("/es")), "es");
});

test("an address naming no locale, or one the middleware never saw, falls back to the default", () => {
  assert.equal(notFoundLocale(requestFor("/fr/x")), "en");
  assert.equal(notFoundLocale(requestFor("/espanol/x")), "en");
  assert.equal(notFoundLocale(requestFor(null)), "en");
});

test("each locale has the screen's own words, and nothing to retry or quote", () => {
  // Nothing failed, so there is no retry and no reference code to word.
  for (const locale of LOCALES) {
    assert.deepEqual(Object.keys(notFoundCopy(locale)).sort(), ["back", "description", "title"], locale);
  }
  const [en, es] = [notFoundCopy("en"), notFoundCopy("es")];
  for (const key of ["title", "description", "back"] as const) {
    assert.notEqual(es[key], en[key], `notFound.${key} is the same in both catalogs`);
  }
  assert.match(es.title, /página/i);
  assert.match(en.title, /page/i);
});
