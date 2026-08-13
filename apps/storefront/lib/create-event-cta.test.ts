/**
 * The one decision behind the "Create an event" invitation: where it points,
 * and whether it is offered at all.
 *
 * The failure this file exists for is a link to nowhere. The staff origin
 * arrives as runtime configuration, which means it can be absent (local dev,
 * a preview stack), blank (a terraform variable that resolved to ""), or
 * mangled (a domain pasted without a scheme). None of those may produce a
 * footer link a reader can press into a 404, and none of them may throw during
 * a server render — every one of them must produce nothing at all, so the
 * component upstream renders no link and the footer is exactly as it was.
 */

import assert from "node:assert/strict";
import test from "node:test";

// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import { createEventCtaHref } from "./create-event-cta.ts";

const env = process.env as Record<string, string | undefined>;

/** Runs body with the given environment, restoring whatever was there before. */
function withEnv(vars: Record<string, string | undefined>, body: () => void) {
  const previous = new Map(Object.keys(vars).map((key) => [key, env[key]]));
  for (const [key, value] of Object.entries(vars)) {
    if (value === undefined) delete env[key];
    else env[key] = value;
  }
  try {
    body();
  } finally {
    for (const [key, value] of previous) {
      if (value === undefined) delete env[key];
      else env[key] = value;
    }
  }
}

test("createEventCtaHref points at the configured staff origin's sign-in page, carrying the create intent", () => {
  withEnv({ STAFF_BASE_URL: "https://staff.multiticketing.com" }, () => {
    assert.equal(
      createEventCtaHref(),
      "https://staff.multiticketing.com/login?intent=create",
    );
  });
});

test("createEventCtaHref offers nothing when no staff origin is configured", () => {
  withEnv({ STAFF_BASE_URL: undefined }, () => {
    assert.equal(createEventCtaHref(), undefined);
  });
});

test("createEventCtaHref offers nothing when the staff origin is empty or whitespace", () => {
  withEnv({ STAFF_BASE_URL: "" }, () => {
    assert.equal(createEventCtaHref(), undefined);
  });
  withEnv({ STAFF_BASE_URL: "   \t\n " }, () => {
    assert.equal(createEventCtaHref(), undefined);
  });
});

test("createEventCtaHref offers nothing, rather than throwing, when the staff origin is unparseable", () => {
  // A domain pasted without a scheme is the realistic mistake, and it happens
  // in configuration nobody renders locally — so it must degrade, not crash a
  // page that has nothing to do with the invitation.
  for (const raw of ["staff.multiticketing.com", "https://", "://nope", "not a url"]) {
    withEnv({ STAFF_BASE_URL: raw }, () => {
      assert.equal(createEventCtaHref(), undefined, `expected nothing for ${raw}`);
    });
  }
});

test("createEventCtaHref reads a trailing slash on the staff origin as the same origin", () => {
  let withSlash: string | undefined;
  let withoutSlash: string | undefined;
  withEnv({ STAFF_BASE_URL: "https://staff.multiticketing.com/" }, () => {
    withSlash = createEventCtaHref();
  });
  withEnv({ STAFF_BASE_URL: "https://staff.multiticketing.com" }, () => {
    withoutSlash = createEventCtaHref();
  });

  assert.equal(withSlash, withoutSlash);
  assert.equal(withSlash, "https://staff.multiticketing.com/login?intent=create");
});

test("createEventCtaHref keeps the staff origin's port, so a local stack never links to production", () => {
  withEnv({ STAFF_BASE_URL: "http://localhost:64301" }, () => {
    assert.equal(createEventCtaHref(), "http://localhost:64301/login?intent=create");
  });
});
