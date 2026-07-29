import assert from "node:assert/strict";
import test from "node:test";

import {
  checkoutContextLocale,
  checkoutLocaleFromReferer,
  parseCheckoutContext,
  serializeCheckoutContext,
  type CheckoutContext,
} from "./checkout-context-cookie.ts";

const context: CheckoutContext = {
  clientTransactionId: "ctid-1",
  eventPath: "/demo-venue/events/midnight",
  eventName: "Midnight Set",
  customerEmail: "buyer@example.com",
  locale: "es",
};

test("a checkout's context survives the round trip through the cookie", () => {
  const stored = serializeCheckoutContext(context);
  assert.deepEqual(parseCheckoutContext(stored), context);
  assert.equal(checkoutContextLocale(stored), "es");

  const english = serializeCheckoutContext({ ...context, locale: "en" });
  assert.equal(checkoutContextLocale(english), "en");
});

// Everything below is the return leg from the Payment Provider, which runs after
// a buyer has already paid. Nothing here may throw, and nothing may claim a
// language the cookie did not actually state: null hands the decision back to
// the switcher-cookie/Accept-Language chain, which is where it stood before the
// field existed.

test("a cookie written before checkouts remembered a language states none", () => {
  // Verbatim the shape shipped before T5 — a real buyer mid-checkout across the
  // deploy comes back holding exactly this.
  const older = JSON.stringify({
    clientTransactionId: "ctid-1",
    eventPath: "/demo-venue/events/midnight",
    eventName: "Midnight Set",
    customerEmail: "buyer@example.com",
  });

  assert.equal(checkoutContextLocale(older), null);
  // And the rest of it still reads, because the terminal pages depend on it.
  assert.deepEqual(parseCheckoutContext(older), { ...context, locale: null });
});

test("a language this Storefront does not serve is not a language", () => {
  for (const locale of ["fr", "es-EC", "ES", "", null, 42, { locale: "es" }]) {
    const stored = JSON.stringify({ ...context, locale });
    assert.equal(checkoutContextLocale(stored), null);
    assert.equal(parseCheckoutContext(stored)?.locale, null);
  }
});

test("a cookie that is not a context at all is simply nothing", () => {
  for (const raw of ["", "not json", "[]", '"es"', "null", "42", undefined, null]) {
    assert.equal(checkoutContextLocale(raw), null);
    assert.equal(parseCheckoutContext(raw), null);
  }
});

test("the language outlives damage to the rest of the context", () => {
  // The event path fails its guard, so there is no context for the terminal
  // pages to work from — but the buyer still gets their own language back.
  const damaged = JSON.stringify({ ...context, eventPath: "https://evil.example/x" });
  assert.equal(parseCheckoutContext(damaged), null);
  assert.equal(checkoutContextLocale(damaged), "es");
});

// Where the language comes from in the first place: the Event page that called
// the begin-checkout route, named by the Referer of that call.

test("the beginning language is the prefix of the page that began the checkout", () => {
  assert.equal(
    checkoutLocaleFromReferer("https://tickets.example/es/demo-venue/events/midnight"),
    "es",
  );
  assert.equal(
    checkoutLocaleFromReferer("https://tickets.example/en/demo-venue/events/x?a=1"),
    "en",
  );
  assert.equal(checkoutLocaleFromReferer("http://localhost:3000/es"), "es");
});

test("a Referer naming no language leaves the guess where it was", () => {
  assert.equal(checkoutLocaleFromReferer("https://tickets.example/demo-venue/events/x"), null);
  assert.equal(checkoutLocaleFromReferer("https://tickets.example/es-EC/x"), null);
  assert.equal(checkoutLocaleFromReferer("https://tickets.example/"), null);
  // Stripped by a privacy extension, or a policy that sends the origin only.
  assert.equal(checkoutLocaleFromReferer(null), null);
  assert.equal(checkoutLocaleFromReferer(undefined), null);
  assert.equal(checkoutLocaleFromReferer(""), null);
  assert.equal(checkoutLocaleFromReferer("/es/demo-venue/events/x"), null);
});
