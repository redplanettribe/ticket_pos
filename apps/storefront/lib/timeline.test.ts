/**
 * The Timeline's grouping: which group each Event of the explorer's feed lands
 * in (CONTEXT.md: Timeline, Day Bucket, Ongoing).
 *
 * A wrong grouping is invisible downstream — the page still renders a
 * plausible-looking timeline with an Event under the wrong day, or a running
 * festival missing from Ongoing — so the boundaries are asserted here, against
 * a fixed `now`, which is why buildTimeline takes one.
 */

import assert from "node:assert/strict";
import test from "node:test";

import type { PublicEventCard } from "./api.ts";
// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import { buildTimeline } from "./timeline.ts";

/** A card with everything the grouping does not read stubbed out. */
function card(overrides: Partial<PublicEventCard> & { slug: string }): PublicEventCard {
  return {
    name: overrides.slug,
    starts_at: null,
    ends_at: null,
    timezone: "America/Guayaquil",
    venue_name: null,
    cover_image_url: null,
    organization: { name: "Org", slug: "org", logo_url: null },
    currency: "USD",
    price_from_cents: null,
    sold_out: false,
    all_closed: false,
    tickets_sold: null,
    tags: [],
    registration_mode: "tickets",
    ...overrides,
  };
}

test("an Event is bucketed by its own local calendar date, not the instant's UTC date", () => {
  // One and the same instant: 03:00 UTC on Aug 8. In Guayaquil (UTC-5) that is
  // still the evening of Aug 7; in Madrid (UTC+2, summer) it is the small hours
  // of Aug 8. Two Events starting at that instant belong to different days.
  const now = new Date("2026-08-03T12:00:00Z");
  const timeline = buildTimeline(
    [
      card({ slug: "guayaquil", starts_at: "2026-08-08T03:00:00Z", timezone: "America/Guayaquil" }),
      card({ slug: "madrid", starts_at: "2026-08-08T03:00:00Z", timezone: "Europe/Madrid" }),
    ],
    now,
  );

  assert.deepEqual(
    timeline.days.map((day) => [day.key, day.events.map((e) => e.slug)]),
    [
      ["2026-08-07", ["guayaquil"]],
      ["2026-08-08", ["madrid"]],
    ],
  );
  assert.deepEqual(timeline.ongoing, []);
});

test("Ongoing holds exactly the Events whose runtime contains now: start passed, end not reached", () => {
  const now = new Date("2026-08-03T20:00:00Z");
  const timeline = buildTimeline(
    [
      // A festival mid-run: started yesterday, ends tomorrow.
      card({ slug: "mid-run", starts_at: "2026-08-02T20:00:00Z", ends_at: "2026-08-04T20:00:00Z" }),
      // Starting at this very instant: the runtime has begun, start-inclusive.
      card({ slug: "at-start", starts_at: "2026-08-03T20:00:00Z", ends_at: "2026-08-04T02:00:00Z" }),
      // Ending at this very instant: over, end-exclusive — not ongoing.
      card({ slug: "at-end", starts_at: "2026-08-03T02:00:00Z", ends_at: "2026-08-03T20:00:00Z" }),
      // Started but with no end: never Ongoing — nothing says it is still running.
      card({ slug: "no-end", starts_at: "2026-08-03T19:00:00Z", ends_at: null }),
      // Plainly in the future.
      card({ slug: "future", starts_at: "2026-08-05T20:00:00Z", ends_at: "2026-08-05T23:00:00Z" }),
    ],
    now,
  );

  assert.deepEqual(
    timeline.ongoing.map((e) => e.slug),
    ["mid-run", "at-start"],
  );
  // Everyone else stands under the Day Bucket of their start date — an Event
  // appears in exactly one place in the Timeline.
  assert.deepEqual(
    timeline.days.map((day) => [day.key, day.events.map((e) => e.slug)]),
    [
      ["2026-08-02", ["at-end"]],
      ["2026-08-03", ["no-end"]],
      ["2026-08-05", ["future"]],
    ],
  );
});

test("'today' and 'tomorrow' are judged by the platform wall clock, not UTC", () => {
  // 03:00 UTC on Aug 4 is still Aug 3, 22:00 in Ecuador. The platform's today
  // is Aug 3 — a UTC-based judgement would shift both labels one day forward.
  const now = new Date("2026-08-04T03:00:00Z");
  const timeline = buildTimeline(
    [
      // Aug 3, 23:00 in Guayaquil: the platform's today.
      card({ slug: "tonight", starts_at: "2026-08-04T04:00:00Z" }),
      // Aug 4, 18:00 in Guayaquil: the platform's tomorrow.
      card({ slug: "tomorrow-night", starts_at: "2026-08-04T23:00:00Z" }),
      // Aug 5: just a date.
      card({ slug: "later", starts_at: "2026-08-05T23:00:00Z" }),
    ],
    now,
  );

  assert.deepEqual(
    timeline.days.map((day) => [day.key, day.relative]),
    [
      ["2026-08-03", "today"],
      ["2026-08-04", "tomorrow"],
      ["2026-08-05", null],
    ],
  );
});

test("the labels roll over at the platform's midnight, not at UTC's", () => {
  // Half an hour after Ecuador's midnight (05:30 UTC), Aug 4 has become today
  // and Aug 5 tomorrow — the mirror of the pre-midnight case above.
  const now = new Date("2026-08-04T05:30:00Z");
  const timeline = buildTimeline(
    [
      card({ slug: "tonight", starts_at: "2026-08-04T23:00:00Z" }),
      card({ slug: "tomorrow-night", starts_at: "2026-08-05T23:00:00Z" }),
    ],
    now,
  );

  assert.deepEqual(
    timeline.days.map((day) => [day.key, day.relative]),
    [
      ["2026-08-04", "today"],
      ["2026-08-05", "tomorrow"],
    ],
  );
});

test("regrouping two concatenated pages merges a day split across the boundary", () => {
  // "Load more" appends a page and regroups the whole array — there is no merge
  // step to get wrong. A day cut in half by the page boundary must come out as
  // one Day Bucket, its Events still in feed order, never a repeated header.
  const now = new Date("2026-08-03T12:00:00Z");
  const pageOne = [
    card({ slug: "first", starts_at: "2026-08-10T23:00:00Z" }),
    card({ slug: "second", starts_at: "2026-08-11T00:00:00Z" }), // Aug 10, 19:00 in Guayaquil
  ];
  const pageTwo = [
    card({ slug: "third", starts_at: "2026-08-11T01:00:00Z" }), // Aug 10, 20:00 in Guayaquil
    card({ slug: "fourth", starts_at: "2026-08-12T00:00:00Z" }), // Aug 11, 19:00 in Guayaquil
  ];

  const timeline = buildTimeline([...pageOne, ...pageTwo], now);

  assert.deepEqual(
    timeline.days.map((day) => [day.key, day.events.map((e) => e.slug)]),
    [
      ["2026-08-10", ["first", "second", "third"]],
      ["2026-08-11", ["fourth"]],
    ],
  );
});
