import assert from "node:assert/strict";
import test from "node:test";

import { registrationClickPath, registrationHandoff } from "./registration-click.ts";

const external = (url: string | null) => ({
  registration_mode: "external",
  registration_url: url,
});

test("the hand-off route is the Event page's own path with one segment on the end", () => {
  assert.equal(registrationClickPath("teatro-sucre", "noche-de-jazz"), "/teatro-sucre/events/noche-de-jazz/register");
});

// Slugs come out of our own URLs, but they are still values that end up in one:
// nothing here may let a slug break out of the path it belongs in.
test("a slug carrying path characters is escaped into its segment", () => {
  assert.equal(registrationClickPath("a/b", "c?d"), "/a%2Fb/events/c%3Fd/register");
});

test("an externally registered Event hands off to its Registration Link", () => {
  assert.deepEqual(registrationHandoff(external("https://lu.ma/noche")), {
    href: "https://lu.ma/noche",
  });
});

// The route's not-found cases, which are all the same answer: there is no
// hand-off at this address.
test("an Event with no hand-off to make answers none", () => {
  assert.equal(registrationHandoff(null), null, "an Event the API does not have");
  assert.equal(
    registrationHandoff({ registration_mode: "tickets", registration_url: null }),
    null,
    "an Event that sells Ticket Types here",
  );
  assert.equal(registrationHandoff(external(null)), null, "a Registration Link not filled in yet");
  assert.equal(registrationHandoff(external("   ")), null, "a blank Registration Link");
});

// A ticketed Event is not made external by having a URL on it, and a mode this
// version does not know reads as ticketed — the same safe answer the Event page
// gives it.
test("only the mode decides whether there is a hand-off, never the link", () => {
  assert.equal(
    registrationHandoff({ registration_mode: "tickets", registration_url: "https://lu.ma/noche" }),
    null,
  );
  assert.equal(
    registrationHandoff({ registration_mode: "hybrid", registration_url: "https://lu.ma/noche" }),
    null,
  );
});

// This value becomes a Location header. The backend refuses to store anything
// but https, and this is the second, independent enforcement of that rule: a row
// written before the rule, or by a compromise upstream, must produce a
// not-found and never a redirect a browser would act on.
test("a stored link a browser must not be sent to is no hand-off at all", () => {
  for (const href of [
    "javascript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "http://lu.ma/noche",
    "//evil.example.com",
    "lu.ma/noche",
  ]) {
    assert.equal(registrationHandoff(external(href)), null, href);
  }
});
