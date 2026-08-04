import assert from "node:assert/strict";
import test from "node:test";

import { isExternallyRegistered, registrationDestination } from "./registration.ts";

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
