import assert from "node:assert/strict";
import test from "node:test";

import {
  EVENTS_TAB_KEYS,
  eventsEmptyState,
  eventsTab,
  eventsTabCounts,
  eventsTabHref,
  isOver,
  partitionEvents,
  type TabbableEvent,
} from "./events-tabs.ts";

/** The instant every case below is judged at, so nothing here reads a real clock. */
const NOW = new Date("2026-09-11T17:00:00Z");

const HOUR_BEFORE = "2026-09-11T16:00:00Z";
const HOUR_AFTER = "2026-09-11T18:00:00Z";
const LAST_MARCH = "2026-03-04T20:00:00Z";
const NEXT_MONTH = "2026-10-04T20:00:00Z";

/** An Event as the tabs read one: a status, a start and an end. */
function event(
  fields: Partial<TabbableEvent> & { id?: string } = {},
): TabbableEvent & { id: string } {
  return {
    id: "e",
    status: "published",
    starts_at: null,
    ends_at: null,
    ...fields,
  };
}

test("a cancelled Event is in Cancelled whatever its dates", () => {
  // Cancelling outranks the clock (ADR 0071): neither an ancient call-off nor
  // one for next week falls through into a tab decided by the date.
  const longPast = event({ status: "cancelled", starts_at: LAST_MARCH, ends_at: LAST_MARCH });
  const future = event({ status: "cancelled", starts_at: NEXT_MONTH, ends_at: NEXT_MONTH });
  const dateless = event({ status: "cancelled" });

  assert.equal(eventsTab(longPast, NOW), "cancelled");
  assert.equal(eventsTab(future, NOW), "cancelled");
  assert.equal(eventsTab(dateless, NOW), "cancelled");
});

test("a published Event is placed by its end instant, not its start", () => {
  const finished = event({ starts_at: HOUR_BEFORE, ends_at: HOUR_BEFORE });
  const ahead = event({ starts_at: HOUR_AFTER, ends_at: HOUR_AFTER });
  // Mid-run: started this morning, ends tomorrow. Active is defined by the end.
  const running = event({ starts_at: HOUR_BEFORE, ends_at: HOUR_AFTER });

  assert.equal(eventsTab(finished, NOW), "past");
  assert.equal(eventsTab(ahead, NOW), "active");
  assert.equal(eventsTab(running, NOW), "active");
});

test("an Event with no end falls back to its start", () => {
  assert.equal(eventsTab(event({ starts_at: HOUR_BEFORE }), NOW), "past");
  assert.equal(eventsTab(event({ starts_at: HOUR_AFTER }), NOW), "active");
});

test("publishing has no say in which tab an Event lands in", () => {
  // A draft for next month sits beside the Events already selling; a draft
  // dated last March and never published is over, which is all Past means.
  assert.equal(eventsTab(event({ status: "draft", starts_at: NEXT_MONTH }), NOW), "active");
  assert.equal(eventsTab(event({ status: "draft", starts_at: LAST_MARCH }), NOW), "past");
});

test("an Event with no start at all is never over", () => {
  assert.equal(isOver(event({ status: "draft" }), NOW), false);
  assert.equal(eventsTab(event({ status: "draft" }), NOW), "active");
});

test("the boundary instant itself is not yet past", () => {
  const atNow = { status: "published", starts_at: NOW.toISOString(), ends_at: NOW.toISOString() };
  // `COALESCE(ends_at, starts_at) >= now` is the filter the global explorer
  // ships, so the staff tabs and the Storefront agree by construction: an
  // Event ending exactly now has not ended yet.
  assert.equal(isOver(atNow, NOW), false);
  assert.equal(eventsTab(atNow, NOW), "active");
  assert.equal(isOver({ ...atNow, ends_at: "2026-09-11T16:59:59Z" }, NOW), true);

  // The offset is the API's to choose: the same instant written two ways is
  // the same instant. The Event's timezone decides how a date is printed and
  // never whether it has passed.
  assert.equal(isOver({ status: "published", starts_at: null, ends_at: "2026-09-11T12:00:00-05:00" }, NOW), false);
});

test("a date this app cannot read is no grounds for calling an Event over", () => {
  assert.equal(isOver({ status: "published", starts_at: "not an instant", ends_at: null }, NOW), false);
  assert.equal(eventsTab({ status: "published", starts_at: "not an instant", ends_at: null }, NOW), "active");
});

test("Active is ascending by start with the dateless Events pinned above", () => {
  const { active } = partitionEvents(
    [
      event({ id: "october", starts_at: NEXT_MONTH }),
      event({ id: "dateless", status: "draft" }),
      event({ id: "tomorrow", starts_at: HOUR_AFTER }),
      event({ id: "dateless-two", status: "draft" }),
    ],
    NOW,
  );

  assert.deepEqual(
    active.map((e) => e.id),
    ["dateless", "dateless-two", "tomorrow", "october"],
  );
});

test("Past and Cancelled are descending by start with the dateless Events last", () => {
  const { past, cancelled } = partitionEvents(
    [
      event({ id: "march", starts_at: LAST_MARCH }),
      event({ id: "cancelled-dateless", status: "cancelled" }),
      event({ id: "this-morning", starts_at: HOUR_BEFORE }),
      event({ id: "cancelled-march", status: "cancelled", starts_at: LAST_MARCH }),
      event({ id: "cancelled-october", status: "cancelled", starts_at: NEXT_MONTH }),
    ],
    NOW,
  );

  assert.deepEqual(
    past.map((e) => e.id),
    ["this-morning", "march"],
  );
  assert.deepEqual(
    cancelled.map((e) => e.id),
    ["cancelled-october", "cancelled-march", "cancelled-dateless"],
  );
});

test("a tie keeps the order it arrived in, so repeated renders do not shuffle", () => {
  const input = [
    event({ id: "a", starts_at: NEXT_MONTH }),
    event({ id: "b", starts_at: NEXT_MONTH }),
    event({ id: "c", starts_at: NEXT_MONTH }),
  ];

  assert.deepEqual(
    partitionEvents(input, NOW).active.map((e) => e.id),
    ["a", "b", "c"],
  );
  assert.deepEqual(
    partitionEvents(input.map((e) => ({ ...e, status: "cancelled" })), NOW).cancelled.map(
      (e) => e.id,
    ),
    ["a", "b", "c"],
  );
});

test("the three tabs hold every Event exactly once", () => {
  const input = [
    event({ id: "1", status: "cancelled", starts_at: LAST_MARCH }),
    event({ id: "2", status: "cancelled", starts_at: NEXT_MONTH }),
    event({ id: "3", starts_at: HOUR_BEFORE, ends_at: HOUR_AFTER }),
    event({ id: "4", starts_at: LAST_MARCH, ends_at: LAST_MARCH }),
    event({ id: "5", status: "draft" }),
    event({ id: "6", status: "draft", starts_at: NEXT_MONTH }),
    event({ id: "7", starts_at: "not an instant" }),
    // A status this app has never heard of is still somebody's Event, and the
    // tabs promise to hold it. It is not cancelled, so the clock places it.
    event({ id: "8", status: "archived", starts_at: LAST_MARCH }),
  ];

  const tabs = partitionEvents(input, NOW);
  const placed = EVENTS_TAB_KEYS.flatMap((key) => tabs[key].map((e) => e.id));

  assert.equal(placed.length, input.length);
  assert.deepEqual(new Set(placed), new Set(input.map((e) => e.id)));
});

test("an empty list yields three empty tabs rather than throwing", () => {
  assert.deepEqual(partitionEvents([], NOW), { active: [], past: [], cancelled: [] });
});

test("each tab has an address of its own", () => {
  assert.deepEqual(
    EVENTS_TAB_KEYS.map(eventsTabHref),
    ["/events", "/events/past", "/events/cancelled"],
  );
});

test("the three counts sum to the number of Events the Organization has", () => {
  const input = [
    event({ id: "1", status: "cancelled", starts_at: LAST_MARCH }),
    event({ id: "2", starts_at: NEXT_MONTH, ends_at: NEXT_MONTH }),
    event({ id: "3", starts_at: LAST_MARCH, ends_at: LAST_MARCH }),
    event({ id: "4", status: "draft" }),
  ];

  const counts = eventsTabCounts(partitionEvents(input, NOW));

  assert.deepEqual(counts, { active: 2, past: 1, cancelled: 1 });
  assert.equal(
    EVENTS_TAB_KEYS.reduce((total, key) => total + counts[key], 0),
    input.length,
  );
});

test("a tab holding no Events counts zero rather than dropping out", () => {
  // The reason the counts exist: an Org Admin should be able to tell an empty
  // Cancelled tab from a full one without opening it, so every key is present
  // and every empty one says so.
  const counts = eventsTabCounts(partitionEvents([event({ starts_at: NEXT_MONTH })], NOW));

  assert.deepEqual(counts, { active: 1, past: 0, cancelled: 0 });
  assert.deepEqual(eventsTabCounts(partitionEvents([], NOW)), {
    active: 0,
    past: 0,
    cancelled: 0,
  });
});

test("only an Organization with no Events at all is invited to create its first", () => {
  // `firstRun` is what the page resolves into the existing invitation. An
  // Organization with five Events and none of them active is not on its first
  // run — it has created five — so Active gets a plain statement instead.
  assert.equal(eventsEmptyState("active", false), "firstRun");
  assert.equal(eventsEmptyState("active", true), "emptyTabActive");
});

test("Past and Cancelled never invite, not even on the first run", () => {
  // An Organization with no Events has three empty tabs, and only the one it
  // lands on is the place to ask it to start.
  assert.equal(eventsEmptyState("past", false), "emptyTabPast");
  assert.equal(eventsEmptyState("cancelled", false), "emptyTabCancelled");
  assert.equal(eventsEmptyState("past", true), "emptyTabPast");
  assert.equal(eventsEmptyState("cancelled", true), "emptyTabCancelled");
});

test("each tab states its own emptiness, and no two say the same thing", () => {
  // Whichever tab an Org Admin opens there is a sentence waiting, and it is
  // that tab's sentence: "No past events" on Cancelled would be a tidy way of
  // telling the reader something untrue.
  const states = EVENTS_TAB_KEYS.map((tab) => eventsEmptyState(tab, true));

  assert.equal(new Set(states).size, EVENTS_TAB_KEYS.length);
  assert.equal(
    states.includes("firstRun"),
    false,
    "an Organization that has Events is never invited to create its first",
  );
});
