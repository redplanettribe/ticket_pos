import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_APP_LOCALE,
  appLocaleFromIntl,
  intlLocale,
  isAppLocale,
  resolveLocale,
  toAppLocale,
} from "../src/locale.ts";

// The precedence chain (T4). It decides where an address naming no language
// sends a visitor, and nothing else — a prefixed URL's content never consults
// it, which is what keeps a shared link meaning the same thing at both ends.

test("a deliberate choice beats what the browser says it reads", () => {
  assert.equal(resolveLocale({ cookie: "es", acceptLanguage: "en-US,en;q=0.9" }), "es");
  assert.equal(resolveLocale({ cookie: "en", acceptLanguage: "es-EC,es;q=0.9" }), "en");
});

test("the browser's languages beat the default", () => {
  assert.equal(resolveLocale({ acceptLanguage: "es-EC,es;q=0.9,en;q=0.8" }), "es");
  assert.equal(resolveLocale({ cookie: null, acceptLanguage: "es" }), "es");
});

test("quality weights are read in weight order, not written order", () => {
  assert.equal(resolveLocale({ acceptLanguage: "en;q=0.8,es;q=0.9" }), "es");
  assert.equal(resolveLocale({ acceptLanguage: "es;q=0.4,en;q=0.9" }), "en");
  // Equal weights fall back to the order the header lists them in.
  assert.equal(resolveLocale({ acceptLanguage: "es,en" }), "es");
  assert.equal(resolveLocale({ acceptLanguage: "en,es" }), "en");
  // An unweighted entry is q=1 and outranks every weighted one.
  assert.equal(resolveLocale({ acceptLanguage: "fr;q=1.0,es;q=0.9,en" }), "en");
  // q=0 is an explicit refusal, not a low preference.
  assert.equal(resolveLocale({ acceptLanguage: "es;q=0,fr;q=0.5" }), DEFAULT_APP_LOCALE);
});

test("region and case are read past: there is one Spanish to serve", () => {
  assert.equal(resolveLocale({ acceptLanguage: "es-419" }), "es");
  assert.equal(resolveLocale({ acceptLanguage: "ES-ec" }), "es");
  assert.equal(resolveLocale({ acceptLanguage: "  es-EC  " }), "es");
});

test("a language the platform does not speak falls back to English", () => {
  assert.equal(resolveLocale({ acceptLanguage: "fr-FR,fr;q=0.9,de;q=0.8" }), "en");
  assert.equal(resolveLocale({ acceptLanguage: "*" }), "en");
  assert.equal(resolveLocale({ acceptLanguage: "pt-BR" }), "en");
});

test("a cookie holding anything but a served token is read past", () => {
  assert.equal(resolveLocale({ cookie: "fr", acceptLanguage: "es" }), "es");
  assert.equal(resolveLocale({ cookie: "es-EC", acceptLanguage: "es" }), "es");
  assert.equal(resolveLocale({ cookie: "", acceptLanguage: "es" }), "es");
  assert.equal(resolveLocale({ cookie: "  es  ", acceptLanguage: "en" }), "es");
  // Nothing usable anywhere is still an answer, never a throw.
  assert.equal(resolveLocale({ cookie: "fr" }), "en");
});

test("a malformed or empty header falls back to English and never throws", () => {
  for (const header of [
    "",
    "   ",
    ",,,",
    ";q=0.9",
    "es;q=",
    "es;q=notanumber",
    "=;=;=",
    " ",
    "es".repeat(5000),
  ]) {
    assert.equal(resolveLocale({ acceptLanguage: header }), "en", header.slice(0, 20));
  }
  assert.equal(resolveLocale({}), "en");
  assert.equal(resolveLocale({ cookie: undefined, acceptLanguage: undefined }), "en");
  assert.equal(resolveLocale({ cookie: null, acceptLanguage: null }), "en");
});

test("junk parameters beside a good tag are ignored, not fatal to it", () => {
  // The tag itself is well-formed; parameters that are not a weight simply do
  // not vote, so the entry still counts at its default weight.
  assert.equal(resolveLocale({ acceptLanguage: "es;;;;" }), "es");
  assert.equal(resolveLocale({ acceptLanguage: "es;charset=utf-8" }), "es");
});

test("a malformed weight loses to entries that stated one", () => {
  // "es;q=" fails to say what it is worth, so the entry that did wins — the
  // header is not thrown away wholesale.
  assert.equal(resolveLocale({ acceptLanguage: "es;q=,en;q=0.1" }), "en");
});

// The two vocabularies: the token in the URL, and the tag Intl formats under.

test("the URL token maps to its Intl locale and back", () => {
  assert.equal(intlLocale("en"), "en-US");
  assert.equal(intlLocale("es"), "es-EC");
  assert.equal(appLocaleFromIntl("en-US"), "en");
  assert.equal(appLocaleFromIntl("es-EC"), "es");
  for (const locale of ["en", "es"] as const) {
    assert.equal(appLocaleFromIntl(intlLocale(locale)), locale);
  }
});

test("isAppLocale names the two served tokens and nothing else", () => {
  assert.ok(isAppLocale("en"));
  assert.ok(isAppLocale("es"));
  assert.equal(isAppLocale("es-EC"), false);
  assert.equal(isAppLocale("EN"), false);
  assert.equal(isAppLocale("fr"), false);
  assert.equal(isAppLocale(undefined), false);
  assert.equal(toAppLocale("es"), "es");
  assert.equal(toAppLocale("fr"), DEFAULT_APP_LOCALE);
});
