import assert from "node:assert/strict";
import test from "node:test";

import { localeChoiceCookie } from "./language-switch.ts";
import { LOCALE_COOKIE } from "./locale.ts";

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
