import assert from "node:assert/strict";
import test from "node:test";

import { localeAlternates } from "./alternates.ts";

const BASE = new URL("https://tickets.example.com");

test("the canonical is the page's own address, locale and all", () => {
  // The claim that keeps a Spanish page in Spanish results: /es canonicalises
  // to /es, never sideways to /en.
  assert.equal(
    localeAlternates("/acme/events/gala", "en", BASE).canonical,
    "https://tickets.example.com/en/acme/events/gala",
  );
  assert.equal(
    localeAlternates("/acme/events/gala", "es", BASE).canonical,
    "https://tickets.example.com/es/acme/events/gala",
  );
});

test("every locale lists every locale, itself included", () => {
  const english = localeAlternates("/acme", "en", BASE).languages;
  const spanish = localeAlternates("/acme", "es", BASE).languages;

  assert.deepEqual(english, {
    en: "https://tickets.example.com/en/acme",
    es: "https://tickets.example.com/es/acme",
    "x-default": "https://tickets.example.com/en/acme",
  });
  // Reciprocal: a crawler confirms an hreflang pair from both ends, so the two
  // pages must publish the identical map.
  assert.deepEqual(spanish, english);
});

test("each page's own address appears in its own languages map", () => {
  for (const locale of ["en", "es"] as const) {
    const { canonical, languages } = localeAlternates("/acme", locale, BASE);
    assert.equal(languages[locale], canonical);
  }
});

test("x-default is the English address", () => {
  const { languages } = localeAlternates("/acme/events/gala", "es", BASE);
  // It states where an address naming no language lands, which is English
  // (lib/locale.ts DEFAULT_APP_LOCALE) — not the locale being rendered.
  assert.equal(languages["x-default"], languages.en);
  assert.equal(languages["x-default"], "https://tickets.example.com/en/acme/events/gala");
});

test("the explorer root is a locale and nothing more", () => {
  const { canonical, languages } = localeAlternates("/", "es", BASE);
  assert.equal(canonical, "https://tickets.example.com/es");
  assert.equal(languages.en, "https://tickets.example.com/en");
  assert.equal(languages["x-default"], "https://tickets.example.com/en");
});

test("a path that already names a locale is not prefixed twice", () => {
  // "/en/en/acme" would 404, and would be published as this page's twin.
  const { canonical, languages } = localeAlternates("/es/acme", "en", BASE);
  assert.equal(canonical, "https://tickets.example.com/en/acme");
  assert.equal(languages.es, "https://tickets.example.com/es/acme");
  assert.equal(localeAlternates("/en", "es", BASE).canonical, "https://tickets.example.com/es");
});

test("without an origin the addresses are relative, never wrong", () => {
  // Off-platform STOREFRONT_BASE_URL is unset (lib/site.ts); Next resolves
  // these against metadataBase, exactly as it already does elsewhere.
  const { canonical, languages } = localeAlternates("/acme/events/gala", "es");
  assert.equal(canonical, "/es/acme/events/gala");
  assert.deepEqual(languages, {
    en: "/en/acme/events/gala",
    es: "/es/acme/events/gala",
    "x-default": "/en/acme/events/gala",
  });
  assert.equal(localeAlternates("/", "en").canonical, "/en");
});

test("an origin mounted under a path keeps it", () => {
  const { canonical } = localeAlternates("/acme", "en", new URL("https://example.com/shop/"));
  assert.equal(canonical, "https://example.com/shop/en/acme");
});
