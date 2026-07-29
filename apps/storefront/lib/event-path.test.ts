import assert from "node:assert/strict";
import test from "node:test";

import { eventPageSlugs } from "./event-path.ts";

test("an Event page answers its two slugs, prefixed or not", () => {
  // The unprefixed shape is what an Affiliate Link is written as and what a
  // poster still shows; the prefixed one is what the visitor's browser is on by
  // the time the cookie can be written. Both name the same Event.
  assert.deepEqual(eventPageSlugs("/acme/events/gala"), {
    orgSlug: "acme",
    eventSlug: "gala",
  });
  for (const locale of ["en", "es"]) {
    assert.deepEqual(eventPageSlugs(`/${locale}/acme/events/gala`), {
      orgSlug: "acme",
      eventSlug: "gala",
    });
  }
});

test("a trailing slash does not change the answer", () => {
  assert.deepEqual(eventPageSlugs("/en/acme/events/gala/"), {
    orgSlug: "acme",
    eventSlug: "gala",
  });
});

test("only a whole leading segment counts as a Locale", () => {
  // "enigma" begins with "en" and is an ordinary Organization slug. Reading a
  // Locale off a prefix rather than a segment would strip it and then find one
  // segment too few.
  assert.deepEqual(eventPageSlugs("/enigma/events/gala"), {
    orgSlug: "enigma",
    eventSlug: "gala",
  });
  // An Organization named "es" is unreachable by the same known trade
  // localePrefixOf makes; the point here is that the rule is the same one.
  assert.equal(eventPageSlugs("/es/events/gala"), null);
});

test("addresses that are not Event pages answer null", () => {
  for (const pathname of [
    "/",
    "/en",
    "/en/acme",
    "/en/tickets",
    // The confirmation reference the success page carries is also spelled
    // "?ref=", so this answer is the whole reason a purchase is not filed as an
    // Affiliate Link click.
    "/en/checkout/success",
    "/acme/events",
    // A deeper path under an Event is some other page, not this one.
    "/en/acme/events/gala/checkout",
    // "events" is the literal in the middle, not a slug that may drift.
    "/en/acme/shows/gala",
  ]) {
    assert.equal(eventPageSlugs(pathname), null, `${pathname} was read as an Event page`);
  }
});

test("the slugs come back exactly as the URL spelled them", () => {
  // Verbatim, because every caller already has its own rule for what a slug
  // means — the attribution cookie folds case itself (lib/affiliate-ref.ts) and
  // the API decides whether the Event exists. A second opinion here would only
  // be one more place for the two to disagree.
  assert.deepEqual(eventPageSlugs("/en/Acme/events/Gala-2026"), {
    orgSlug: "Acme",
    eventSlug: "Gala-2026",
  });
});
