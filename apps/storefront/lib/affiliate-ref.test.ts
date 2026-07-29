import assert from "node:assert/strict";
import test from "node:test";

import {
  ATTRIBUTION_WINDOW_DAYS,
  readAffiliateCodes,
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

/** The codes an Event's checkout would carry, newest first. */
function codes(
  jar: string | null,
  event: { orgSlug: string; eventSlug: string },
  at: number,
): string[] {
  return readAffiliateCodes(jar, event.orgSlug, event.eventSlug, at);
}

test("a click on a live Affiliate Link is remembered for that Event's checkout", () => {
  const jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT), ["M4R1A222"]);
});

test("the newest click comes first: last-click Affiliate Attribution", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  jar = click(jar, RAVE, "R4D10SP0", CLICKED_AT + DAY_MS);
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + DAY_MS), ["R4D10SP0", "M4R1A222"]);
});

// The reason the history exists at all (#142, #148). Liveness is not knowable
// at click time, so a click on a code that has since been deactivated must not
// erase the live one clicked before it — the API picks the first live code out
// of the list, and the order says which click was the newest.
test("a dead click does not displace the live one clicked before it", () => {
  let jar = click(null, RAVE, "L1VECODE", CLICKED_AT);
  jar = click(jar, RAVE, "DEADC0DE", CLICKED_AT + DAY_MS);
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + DAY_MS), ["DEADC0DE", "L1VECODE"]);
});

test("a live click after a dead one still wins, because it is newest", () => {
  let jar = click(null, RAVE, "DEADC0DE", CLICKED_AT);
  jar = click(jar, RAVE, "L1VECODE", CLICKED_AT + DAY_MS);
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + DAY_MS), ["L1VECODE", "DEADC0DE"]);
});

test("only the last few clicks on an Event are kept", () => {
  let jar: string | null = null;
  const clicked = ["C0DE0001", "C0DE0002", "C0DE0003", "C0DE0004", "C0DE0005"];
  clicked.forEach((code, i) => {
    jar = click(jar, RAVE, code, CLICKED_AT + i);
  });
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + clicked.length), [
    "C0DE0005",
    "C0DE0004",
    "C0DE0003",
  ]);
});

test("re-clicking a remembered code moves it to the front and restarts its window", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  jar = click(jar, RAVE, "R4D10SP0", CLICKED_AT + DAY_MS);
  const returned = CLICKED_AT + 2 * DAY_MS;
  jar = click(jar, RAVE, "M4R1A222", returned);

  // Once, not twice: the same code is one memory however often it is clicked.
  assert.deepEqual(codes(jar, RAVE, returned), ["M4R1A222", "R4D10SP0"]);
  // And its own window runs from the newest click, so it outlives the other.
  const windowMs = ATTRIBUTION_WINDOW_DAYS * DAY_MS;
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + DAY_MS + windowMs + 1), ["M4R1A222"]);
});

test("each remembered click expires on its own schedule", () => {
  let jar = click(null, RAVE, "0LDC0DE1", CLICKED_AT);
  jar = click(jar, RAVE, "N3WC0DE1", CLICKED_AT + 3 * DAY_MS);
  const windowMs = ATTRIBUTION_WINDOW_DAYS * DAY_MS;

  // Both alive while both windows are open.
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + windowMs - 1), ["N3WC0DE1", "0LDC0DE1"]);
  // The older one drops out on its own day, and the newer one stays.
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + windowMs + 1), ["N3WC0DE1"]);
  // Past the newest click's window, the Event is unattributed again.
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT + 3 * DAY_MS + windowMs + 1), []);
});

test("Affiliate Links are remembered per Event, so one Event never clobbers another", () => {
  let jar = click(null, RAVE, "M4R1A222", CLICKED_AT);
  jar = click(jar, GIG, "R4D10SP0", CLICKED_AT);

  assert.deepEqual(codes(jar, RAVE, CLICKED_AT), ["M4R1A222"]);
  assert.deepEqual(codes(jar, GIG, CLICKED_AT), ["R4D10SP0"]);
  // An Event nobody clicked through to is unattributed, whatever else is
  // remembered.
  assert.deepEqual(readAffiliateCodes(jar, "other-org", "party", CLICKED_AT), []);
});

test("codes are remembered in the case the Affiliate Link issues them in", () => {
  const jar = click(null, RAVE, "  m4r1a222 ", CLICKED_AT);
  assert.deepEqual(codes(jar, RAVE, CLICKED_AT), ["M4R1A222"]);
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
    assert.deepEqual(codes(jar, RAVE, CLICKED_AT), []);
  }
  // And a click after one still lands: nothing here throws.
  const repaired = click("{not json", RAVE, "M4R1A222", CLICKED_AT);
  assert.deepEqual(codes(repaired, RAVE, CLICKED_AT), ["M4R1A222"]);
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
  assert.deepEqual(
    readAffiliateCodes(jar, "demo-venue", "event-59", CLICKED_AT + 59),
    ["M4R1A222"],
  );
});
