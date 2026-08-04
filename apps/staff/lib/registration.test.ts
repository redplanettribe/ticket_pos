import assert from "node:assert/strict";
import test from "node:test";

import { isValidRegistrationURL } from "./registration.ts";

test("a Registration Link may point at any https host, because there is no host allowlist", () => {
  for (const url of [
    "https://lu.ma/my-meetup",
    "https://www.eventbrite.com/e/123",
    "https://docs.google.com/forms/d/e/abc/viewform",
    "https://registration.example.test/sign-up?ref=1#top",
    "  https://lu.ma/my-meetup  ",
  ]) {
    assert.equal(isValidRegistrationURL(url), true, url);
  }
});

test("a Registration Link is refused for every scheme but https", () => {
  // The allowlist is what keeps something executable out of a value destined
  // for both an href and a Location header, and it stops a Customer being
  // downgraded to an insecure connection mid-journey.
  for (const url of [
    "http://lu.ma/my-meetup",
    "javascript:alert(1)",
    "JavaScript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "ftp://example.com/registration",
  ]) {
    assert.equal(isValidRegistrationURL(url), false, url);
  }
});

test("a Registration Link is refused when it is not a URL a browser could follow", () => {
  for (const url of ["", "   ", "lu.ma/my-meetup", "//lu.ma/my-meetup", "https://"]) {
    assert.equal(isValidRegistrationURL(url), false, JSON.stringify(url));
  }
});
