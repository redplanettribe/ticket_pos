import assert from "node:assert/strict";
import test from "node:test";

import {
  ATTRIBUTION_WINDOW_DAYS,
  readAffiliateCode,
  rememberAffiliateClick,
} from "./affiliate-ref.ts";

// One instant to click at, and the same instant plus however long the
// Attribution Window is said to be. The tests derive both from the constant, so
// changing the window changes the rule and not the arithmetic here.
const CLICKED_AT = Date.parse("2026-07-01T12:00:00Z");
const DAY_MS = 24 * 60 * 60 * 1000;

const RAVE = { orgSlug: "demo-venue", eventSlug: "rave" };
const GIG = { orgSlug: "demo-venue", eventSlug: "gig" };

/** Clicks a link and hands back the cookie value the browser would then hold. */
function click(
  jar: string | null,
  event: { orgSlug: string; eventSlug: string },
  code: string,
  at: number,
): string {
  const written = rememberAffiliateClick(jar, event.orgSlug, event.eventSlug, code, at);
  assert.ok(written !== null, `expected ${code} to be remembered`);
  return written;
}

test("a click on a live Affiliate Link is remembered for that Event's checkout", () => {
  const jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  assert.equal(readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT), "M4R1A222");
});

test("the newest click wins: last-click Affiliate Attribution", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  jar = click(jar, RAVE, "R4D10SP0", CLICKED_AT + DAY_MS);
  assert.equal(
    readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT + DAY_MS),
    "R4D10SP0",
  );
});

test("a click is forgotten once the Attribution Window has passed", () => {
  const jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  const windowMs = ATTRIBUTION_WINDOW_DAYS * DAY_MS;

  // Bought on the last day of the window: still credited.
  assert.equal(
    readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT + windowMs - 1),
    "M4R1A222",
  );
  // A minute past it: the click is gone and the sale is unattributed.
  assert.equal(
    readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT + windowMs + 60_000),
    null,
  );
});

test("a later click restarts the Attribution Window on that Event", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  const returned = CLICKED_AT + 6 * DAY_MS;
  jar = click(jar, RAVE, "M4R1A222", returned);
  assert.equal(readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, returned + 5 * DAY_MS), "M4R1A222");
});

test("Affiliate Links are remembered per Event, so one Event never clobbers another", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  jar = click(jar, GIG, "R4D10SP0", CLICKED_AT);

  assert.equal(readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT), "M4R1A222");
  assert.equal(readAffiliateCode(jar, GIG.orgSlug, GIG.eventSlug, CLICKED_AT), "R4D10SP0");
  // An Event nobody clicked through to is unattributed, whatever else is
  // remembered.
  assert.equal(readAffiliateCode(jar, "other-org", "party", CLICKED_AT), null);
});

test("codes are remembered in the case the Affiliate Link issues them in", () => {
  const jar = click(null, RAVE, "  m4r1a222 ", CLICKED_AT);
  assert.equal(readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT), "M4R1A222");
});

test("a ref that could not be a code is not remembered at all", () => {
  for (const junk of ["", "   ", "not a code", "<script>", "x".repeat(200)]) {
    assert.equal(
      rememberAffiliateClick(null, RAVE.orgSlug, RAVE.eventSlug, junk, CLICKED_AT),
      null,
      `expected ${JSON.stringify(junk)} to be refused`,
    );
  }
});

test("an unreadable cookie is treated as no click at all", () => {
  for (const jar of [null, "", "{not json", "[]", '{"demo-venue/rave":"M4R1A222"}']) {
    assert.equal(readAffiliateCode(jar, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT), null);
  }
  // And a click after one still lands: nothing here throws.
  const repaired = click("{not json", RAVE, "M4R1A222", CLICKED_AT);
  assert.equal(readAffiliateCode(repaired, RAVE.orgSlug, RAVE.eventSlug, CLICKED_AT), "M4R1A222");
});

test("the jar does not grow without bound as a visitor browses", () => {
  let jar: string | null = null;
  for (let i = 0; i < 60; i++) {
    jar = click(jar, { orgSlug: "demo-venue", eventSlug: `event-${i}` }, "M4R1A222", CLICKED_AT + i);
  }
  const remembered = Object.keys(JSON.parse(jar as string));
  assert.ok(remembered.length <= 24, `remembered ${remembered.length} Events`);
  // What survives is what was clicked most recently — last click is the whole
  // rule, including which memory is worth keeping.
  assert.equal(readAffiliateCode(jar, "demo-venue", "event-59", CLICKED_AT + 59), "M4R1A222");
});
