import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { UNLOCALIZED_PREFIXES, isUnlocalized } from "./unlocalized-paths.ts";

// One fact, stated twice: the list of addresses that must never gain a locale,
// and the middleware matcher that keeps them from waking the middleware at all.
// Next reads a matcher at build time, so it has to be a literal and cannot be
// derived from the list — which leaves the two to be edited together by hand.
// These tests are what makes forgetting the second edit loud: add a Payment
// Provider callback to UNLOCALIZED_PREFIXES, leave the matcher alone, and the
// first test below fails instead of the callback quietly eating a redirect.
//
// The matcher is read out of middleware.ts rather than restated here. A copy
// would agree with itself forever and prove nothing.

const MIDDLEWARE_SOURCE = readFileSync(new URL("../middleware.ts", import.meta.url), "utf8");

function middlewareMatchers(): RegExp[] {
  const literal = /matcher:\s*(\[[^\]]*\])/.exec(MIDDLEWARE_SOURCE)?.[1];
  assert.ok(literal, "middleware.ts no longer declares a `matcher: [...]` array literal");
  // The literal is written as double-quoted strings with backslash escapes,
  // which is exactly JSON — so the pattern comes back as the string Next itself
  // would receive, escapes and all, without this test re-implementing them.
  const patterns: string[] = JSON.parse(literal);
  assert.ok(patterns.length > 0, "middleware.ts declares an empty matcher");
  // Next compiles each pattern against the whole pathname. Anchoring is a
  // faithful enough stand-in for that here: everything asserted below turns on
  // the negative lookahead, which is plain regex either way.
  return patterns.map((pattern) => new RegExp(`^${pattern}$`));
}

function wakesMiddleware(pathname: string): boolean {
  return middlewareMatchers().some((matcher) => matcher.test(pathname));
}

test("every unlocalized prefix is excluded by the middleware matcher", () => {
  for (const prefix of UNLOCALIZED_PREFIXES) {
    // A route under the prefix, which is the shape every one of these actually
    // arrives in: /api/checkout, /_next/static/…, and the two callback paths,
    // whose exact form the matcher excludes as well.
    const request = `${prefix}/callback`;
    assert.equal(
      wakesMiddleware(request),
      false,
      `${prefix} is in UNLOCALIZED_PREFIXES but config.matcher in middleware.ts still admits ${request}`,
    );
    assert.equal(isUnlocalized(request), true);
  }
});

test("the bare prefix is declined even where the matcher admits it", () => {
  // "/api" and "/_next" with nothing after them slip through the matcher,
  // because it spells those two with a trailing slash on purpose (see the test
  // below). Nothing serves those addresses, but the predicate answers for them
  // anyway — the matcher is an optimization, and the decision is here.
  for (const prefix of UNLOCALIZED_PREFIXES) {
    assert.equal(isUnlocalized(prefix), true);
  }
});

test("ordinary Storefront addresses still reach the middleware", () => {
  // Including an Organization whose slug merely starts with "api": the matcher
  // says "api/" rather than "api" so that this Organization's page is still
  // redirected into a locale rather than silently excluded.
  for (const pathname of ["/", "/en", "/es/tickets", "/apitos/events/summer-fest"]) {
    assert.equal(wakesMiddleware(pathname), true, `${pathname} no longer reaches the middleware`);
    assert.equal(isUnlocalized(pathname), false);
  }
});

test("assets and metadata routes are excluded by both", () => {
  // The other half of the mirrored fact: the matcher's ".*\\..*" and the dot
  // rule in isUnlocalized are the same claim about paths carrying an extension.
  for (const pathname of ["/favicon.ico", "/icon.svg", "/manifest.webmanifest"]) {
    assert.equal(wakesMiddleware(pathname), false);
    assert.equal(isUnlocalized(pathname), true);
  }
});
