import assert from "node:assert/strict";
import test from "node:test";

import { localeChoiceCookie, resolveStaffLocale, STAFF_LOCALE_COOKIE } from "./staff-locale.ts";

/**
 * What a Locale is — the union, the default, and every corner of the
 * Accept-Language ladder (quality ordering, refusal, region and case
 * insensitivity, malformed headers never throwing) — belongs to
 * @ticket-pos/locale and is tested exhaustively there.
 *
 * What is asserted here is only what this app adds: that the pre-authentication
 * ladder is the shared one rather than a reimplementation that drifted, and the
 * exact cookie a click on the login switcher writes.
 *
 * This file exists in lib/ rather than beside i18n/request.ts because the test
 * runner globs `lib/*.test.ts` and nothing else. Locale logic put anywhere else
 * in this app is locale logic no test can see.
 */

test("the cookie beats what the browser asks for", () => {
  // The whole reason the login switcher exists: a Spanish reader on an English
  // laptop clicks once and is not overruled by their own browser on the next
  // request.
  assert.equal(resolveStaffLocale({ cookie: "es", acceptLanguage: "en-US,en;q=0.9" }), "es");
  assert.equal(resolveStaffLocale({ cookie: "en", acceptLanguage: "es-EC,es;q=0.9" }), "en");
});

test("Accept-Language decides when no choice has been made", () => {
  // The arrival this ticket is about: through the Storefront's "Create an event"
  // invitation, from a browser that reads Spanish, having never been here.
  assert.equal(resolveStaffLocale({ acceptLanguage: "es-EC,es;q=0.9,en;q=0.8" }), "es");
  assert.equal(resolveStaffLocale({ acceptLanguage: "en-GB,en;q=0.9" }), "en");
});

test("English is the floor", () => {
  assert.equal(resolveStaffLocale({}), "en");
  assert.equal(resolveStaffLocale({ cookie: null, acceptLanguage: null }), "en");
  // A language the platform does not serve is not a language it can serve.
  assert.equal(resolveStaffLocale({ acceptLanguage: "fr-FR,fr;q=0.9" }), "en");
});

test("a cookie the platform did not write is read past, not honoured", () => {
  // The cookie is ours and a switcher is the only thing that writes it, so
  // anything else in it is stale or hand-made — and must not cost the reader the
  // language their browser did state.
  assert.equal(resolveStaffLocale({ cookie: "fr", acceptLanguage: "es;q=0.9" }), "es");
  assert.equal(resolveStaffLocale({ cookie: "", acceptLanguage: "es;q=0.9" }), "es");
  assert.equal(resolveStaffLocale({ cookie: "es-EC", acceptLanguage: "en" }), "en");
});

test("a malformed Accept-Language never throws", () => {
  // The header is attacker-controlled and arrives mangled from ordinary clients
  // too. A login page that 500s is a person who cannot get in at all.
  for (const header of ["", ";;;", "q=", ",,,", "es;q=notanumber", "x".repeat(5000)]) {
    assert.doesNotThrow(() => resolveStaffLocale({ acceptLanguage: header }), header);
  }
});

test("the choice is written under the name the Storefront also reads", () => {
  // Deliberately the same cookie: somebody who picked Spanish on the Storefront
  // and followed the invitation across is not asked twice.
  assert.equal(STAFF_LOCALE_COOKIE, "NEXT_LOCALE");
  assert.match(localeChoiceCookie("es"), /^NEXT_LOCALE=es;/);
});

test("the choice outlives the visit and is readable from every page", () => {
  const cookie = localeChoiceCookie("es");

  // Site-wide, or the choice made at /login would be invisible everywhere after
  // signing in.
  assert.match(cookie, /Path=\//);
  // A year: the choice is a fact about the person, not about the visit.
  assert.match(cookie, new RegExp(`Max-Age=${60 * 60 * 24 * 365}\\b`));
  // Lax, so the cookie survives the top-level navigation in from the Storefront.
  assert.match(cookie, /SameSite=Lax/);
});

test("Secure is stated rather than assumed", () => {
  // A dev server on plain http must not be handed a cookie the browser silently
  // drops — the switcher would appear to do nothing at all.
  assert.doesNotMatch(localeChoiceCookie("en"), /Secure/);
  assert.doesNotMatch(localeChoiceCookie("en", { secure: false }), /Secure/);
  assert.match(localeChoiceCookie("en", { secure: true }), /Secure/);
});
