import assert from "node:assert/strict";
import test from "node:test";

import { localeChoiceCookie, pathWithQuery } from "./language-switch.ts";
import { LOCALE_COOKIE } from "./locale.ts";

test("the switcher points at the page the visitor is on, not at the explorer", () => {
  assert.equal(pathWithQuery("/acme/events/gala"), "/acme/events/gala");
});

test("the query survives the switch", () => {
  // The explorer's filters live in the query, so dropping it would answer
  // "read this in English" with an unfiltered page.
  assert.equal(pathWithQuery("/", "q=gala&city=quito"), "/?q=gala&city=quito");
  assert.equal(pathWithQuery("/tickets", "page=2"), "/tickets?page=2");
});

test("a query is accepted with or without its leading '?'", () => {
  // `useSearchParams().toString()` gives one form and `location.search` the
  // other; neither may produce "/tickets??page=2".
  assert.equal(pathWithQuery("/tickets", "?page=2"), "/tickets?page=2");
});

test("an empty query leaves no dangling '?'", () => {
  for (const search of ["", "?", null, undefined]) {
    assert.equal(pathWithQuery("/tickets", search), "/tickets");
  }
});

test("the root path keeps its slash for the prefixer to collapse", () => {
  // next-intl's prefixer turns "/" into "/es" and "/?q=x" into "/es?q=x"; it
  // needs the slash to be there to do either.
  assert.equal(pathWithQuery("/"), "/");
  assert.equal(pathWithQuery("/", "q=x"), "/?q=x");
});

test("a path that lost its leading slash is made root-relative again", () => {
  // A relative href would resolve against the current directory and quietly
  // produce /es/acme/en/acme rather than /en/acme.
  assert.equal(pathWithQuery("acme"), "/acme");
  assert.equal(pathWithQuery(""), "/");
});

test("the cookie records the chosen language under the name the middleware reads", () => {
  const cookie = localeChoiceCookie("es");
  assert.ok(cookie.startsWith(`${LOCALE_COOKIE}=es;`), cookie);
});

test("the cookie is site-wide, long-lived and sent on top-level navigations", () => {
  const cookie = localeChoiceCookie("en");
  // Path=/ because the one address that reads this cookie is "/", which is not
  // under the page the choice was made on.
  assert.match(cookie, /(^|; )Path=\/(;|$)/);
  // Lax and not Strict: the cookie has to arrive on the click from an email or
  // a QR code, which is a cross-site GET.
  assert.match(cookie, /(^|; )SameSite=Lax(;|$)/);
  assert.match(cookie, /(^|; )Max-Age=31536000(;|$)/);
});

test("Secure is opt-in so a plain-http dev server is not handed a dropped cookie", () => {
  assert.ok(!localeChoiceCookie("en").includes("Secure"));
  assert.match(localeChoiceCookie("en", { secure: true }), /(^|; )Secure$/);
});
