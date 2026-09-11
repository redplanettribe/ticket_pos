import assert from "node:assert/strict";
import test from "node:test";

import {
  eventCardPriceSlot,
  isExternallyRegistered,
  registrationDestination,
} from "./registration.ts";

test("an Event whose mode is external registers its audience elsewhere", () => {
  assert.equal(
    isExternallyRegistered({
      registration_mode: "external",
      registration_url: "https://lu.ma/my-meetup",
    }),
    true,
  );
});

test("an Event that sells Ticket Types here is not externally registered", () => {
  assert.equal(
    isExternallyRegistered({ registration_mode: "tickets", registration_url: null }),
    false,
  );
});

// The mode is the organizer's choice and the link is what they typed. A page
// that read the link instead would offer a ticket selector for an Event that
// sells nothing, and would treat a mode it has never heard of as external.
test("the mode alone decides, never the presence of a Registration Link", () => {
  assert.equal(
    isExternallyRegistered({ registration_mode: "external", registration_url: null }),
    true,
  );
  assert.equal(
    isExternallyRegistered({
      registration_mode: "tickets",
      registration_url: "https://lu.ma/left-over",
    }),
    false,
  );
  assert.equal(
    isExternallyRegistered({ registration_mode: "luma", registration_url: null }),
    false,
  );
});

test("a Registration Link names where the Customer will continue", () => {
  assert.deepEqual(registrationDestination("https://lu.ma/my-meetup"), {
    href: "https://lu.ma/my-meetup",
    hostname: "lu.ma",
  });
  assert.deepEqual(
    registrationDestination("https://docs.google.com/forms/d/e/abc/viewform?usp=sf_link"),
    {
      href: "https://docs.google.com/forms/d/e/abc/viewform?usp=sf_link",
      hostname: "docs.google.com",
    },
  );
});

// "www." says nothing about which site this is, and the shorter name is the one
// a Customer recognises. Only the leading one goes: www.example.com is
// example.com, and my-www.example.com is left alone.
test("the destination is named without its www prefix", () => {
  assert.equal(registrationDestination("https://www.eventbrite.com/e/123")?.hostname, "eventbrite.com");
  assert.equal(registrationDestination("https://my-www.example.com/x")?.hostname, "my-www.example.com");
});

// The href travels untouched even when the name shown beside it is shortened:
// the Customer is sent exactly where the organizer pointed them.
test("the link followed is the link the organizer typed", () => {
  assert.equal(
    registrationDestination("  https://www.eventbrite.com/e/123?aff=x#tickets  ")?.href,
    "https://www.eventbrite.com/e/123?aff=x#tickets",
  );
});

// This value becomes an href. The backend refuses everything but https on the
// way in; restating it here is deliberate, so a row that predates that guard —
// or a payload that got past it — produces no link rather than a link that runs.
test("a Registration Link that is not https offers nowhere to go", () => {
  for (const url of [
    "javascript:alert(document.cookie)",
    "JavaScript:alert(1)",
    "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
    "http://lu.ma/my-meetup",
    "ftp://example.com/registration",
    "//lu.ma/my-meetup",
    "lu.ma/my-meetup",
    "https://",
  ]) {
    assert.equal(registrationDestination(url), null, url);
  }
});

test("an Event with no Registration Link yet offers nowhere to go", () => {
  assert.equal(registrationDestination(null), null);
  assert.equal(registrationDestination(undefined), null);
  assert.equal(registrationDestination(""), null);
  assert.equal(registrationDestination("   "), null);
});

// The listing card's price slot (issue #211). A card with no price line at all
// is how a broken Event looks, so an externally registered one — which will
// never have a price here — gets words in that slot instead of a blank.

test("an external Event's card says registration is required rather than nothing", () => {
  assert.deepEqual(
    eventCardPriceSlot({
      registration_mode: "external",
      registration_url: "https://lu.ma/my-meetup",
      price_from_cents: null,
      currency: "USD",
    }),
    { kind: "registration" },
  );
});

// The one wrong answer that looks like a right one. The other site may well
// charge; this platform does not know what, and never will, so nothing about
// the money may be implied in either direction.
test("an external Event is never called free and is never given a price", () => {
  const slot = eventCardPriceSlot({
    registration_mode: "external",
    registration_url: "https://lu.ma/my-meetup",
    // Even an amount that somehow reached the payload is not a price of a sale
    // this platform is making, so it is not quoted.
    price_from_cents: 0,
    currency: "USD",
  });
  assert.deepEqual(slot, { kind: "registration" });
});

test("a ticketed Event's card quotes its cheapest ticket exactly as before", () => {
  assert.deepEqual(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: 2500,
      currency: "USD",
    }),
    { kind: "from", price: "$25" },
  );
  assert.deepEqual(
    eventCardPriceSlot(
      {
        registration_mode: "tickets",
        registration_url: null,
        price_from_cents: 2500,
        currency: "USD",
      },
      "es-EC",
    ),
    { kind: "from", price: "$25" },
  );
});

test("a ticketed Event with a free ticket still says Free", () => {
  assert.deepEqual(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: 0,
      currency: "USD",
    }),
    { kind: "free" },
  );
});

// A ticketed Event with no priced Ticket Type is a data anomaly and has no
// claim to make; it keeps saying nothing, exactly as it always has.
test("a ticketed Event with no price says nothing at all", () => {
  assert.equal(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: null,
      currency: "USD",
    }),
    null,
  );
});

test("an Event whose every Ticket Type has closed quotes no price at all", () => {
  // The "from" price is computed over the Ticket Types still open in time, so
  // closing the last one empties it (ADR 0070): there is no price left that a
  // Customer could actually pay, and last week's would advertise a sale that has
  // ended. The card's corner says "Sales closed" in words, which is what stops
  // the quiet slot from reading as a card that failed to load.
  assert.equal(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: null,
      currency: "USD",
      all_closed: true,
    }),
    null,
  );
});

test("an all-closed Event is still never quoted a price that reached its payload", () => {
  // The closed branch is asked before the amount, exactly as the external one
  // is, so a figure arriving on a card whose sales have ended cannot be quoted.
  assert.equal(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: 2500,
      currency: "USD",
      all_closed: true,
    }),
    null,
  );
});

test("an Event with Ticket Types still open prices exactly as it always has", () => {
  // all_closed false, and absent, are the same statement: nothing about a card
  // that has not closed changes.
  const expected = { kind: "from", price: "$25" };
  assert.deepEqual(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: 2500,
      currency: "USD",
      all_closed: false,
    }),
    expected,
  );
  assert.deepEqual(
    eventCardPriceSlot({
      registration_mode: "tickets",
      registration_url: null,
      price_from_cents: 2500,
      currency: "USD",
    }),
    expected,
  );
});
