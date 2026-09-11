import assert from "node:assert/strict";
import test from "node:test";

import {
  formatSalesCutoff,
  ticketTypeBadge,
  type BadgeableTicketType,
  type TicketTypeBadge,
} from "./sales-cutoff.ts";

const CUTOFF = "2026-07-09T12:00:00Z";

/** A Ticket Type the server called open: on sale, in stock, unrationed. */
function open(overrides: Partial<BadgeableTicketType> = {}): BadgeableTicketType {
  return { sold_out: false, closed: false, sales_cutoff_at: null, ...overrides };
}

test("the closing time is read on the Event's clock, not the viewer's", () => {
  // Noon UTC is 7:00 AM in Guayaquil and 2:00 PM in Madrid. The organizer set
  // one instant in the Event's timezone, and every buyer must be told that same
  // wall time however far away they are standing (ADR 0070).
  assert.match(formatSalesCutoff(CUTOFF, "America/Guayaquil") ?? "", /^Thu Jul 9.* 7:00 AM$/);
  assert.match(formatSalesCutoff(CUTOFF, "Europe/Madrid") ?? "", /^Thu Jul 9.* 2:00 PM$/);
});

test("the closing time is stated in the reader's language", () => {
  // The Storefront never shows the API's English: the date is formatted for the
  // Locale the page is being read in, as every other date on it is.
  assert.match(formatSalesCutoff(CUTOFF, "America/Guayaquil", "es-EC") ?? "", /jul/);
});

test("a Ticket Type with no Sales Cutoff has no closing line", () => {
  // The state every Ticket Type is in today. Null is "this never stops selling"
  // and gets no sentence at all, rather than a blank one.
  assert.equal(formatSalesCutoff(null, "America/Guayaquil"), null);
});

// --- The countdown ladder --------------------------------------------------
//
// Asserted on the rung the ladder lands on and never on a formatted date: this
// machine runs a different ICU from CI and production, and an Intl comparison
// that passes here can fail there over a space nobody can see.

const ZONE = "America/Guayaquil";

/** An instant on a Guayaquil (UTC-5) calendar date, at the hour given. */
function guayaquil(date: string, hour = 12): Date {
  return new Date(`${date}T${String(hour).padStart(2, "0")}:00:00-05:00`);
}

test("a cutoff later today reads as closing today", () => {
  // The most urgent rung, and the one a number would read worst as: "0 days
  // left" is not something anybody says.
  assert.deepEqual(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-10", 23).toISOString() }), {
      timezone: ZONE,
      now: guayaquil("2026-07-10", 9),
    }),
    { kind: "closes_today" },
  );
});

test("a cutoff on the next calendar date reads as closing tomorrow", () => {
  assert.deepEqual(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-11", 1).toISOString() }), {
      timezone: ZONE,
      now: guayaquil("2026-07-10", 23),
    }),
    { kind: "closes_tomorrow" },
  );
});

test("the numbered rungs run from two days out to seven", () => {
  // Both ends of the ladder. Two is the first rung that gets a number, and
  // seven is the last rung there is.
  for (const days of [2, 3, 4, 5, 6, 7]) {
    const cutoff = guayaquil("2026-07-10");
    cutoff.setUTCDate(cutoff.getUTCDate() + days);
    assert.deepEqual(
      ticketTypeBadge(open({ sales_cutoff_at: cutoff.toISOString() }), {
        timezone: ZONE,
        now: guayaquil("2026-07-10", 9),
      }),
      { kind: "days_left", days },
      `${days} days out`,
    );
  }
});

test("the eighth day out says nothing at all", () => {
  // The boundary. A deadline a Customer cannot act on this week is not news,
  // and a badge that is always on stops being read.
  assert.equal(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-18").toISOString() }), {
      timezone: ZONE,
      now: guayaquil("2026-07-10", 9),
    }),
    null,
  );
});

test("the days are counted on the Event's calendar, not in 24-hour blocks", () => {
  // 8 PM Friday in Guayaquil to a cutoff at 8 AM Saturday is twelve hours — less
  // than one 24-hour block — and still "closes tomorrow", because that is what
  // the Event's calendar says and what it will go on saying all evening.
  assert.deepEqual(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-11", 8).toISOString() }), {
      timezone: ZONE,
      now: guayaquil("2026-07-10", 20),
    }),
    { kind: "closes_tomorrow" },
  );
});

test("a buyer in Madrid and one in Quito are told the same thing", () => {
  // The countdown is a fact about the Event, so the same instant and the same
  // Event calendar produce the same rung wherever the reader is standing. The
  // viewer's zone never enters the arithmetic.
  const cutoff = guayaquil("2026-07-12", 18).toISOString();
  const now = guayaquil("2026-07-10", 22); // already the 11th in Madrid
  assert.deepEqual(ticketTypeBadge(open({ sales_cutoff_at: cutoff }), { timezone: ZONE, now }), {
    kind: "days_left",
    days: 2,
  });
});

test("a Ticket Type with no Sales Cutoff never counts down", () => {
  // The state every Ticket Type is in today: nothing at all changes for it.
  assert.equal(ticketTypeBadge(open(), { timezone: ZONE, now: guayaquil("2026-07-10") }), null);
});

test("an Event with no timezone has no calendar to count on", () => {
  // Rather than falling back to the viewer's calendar, which would tell two
  // buyers different things about one Event.
  assert.equal(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-12").toISOString() }), {
      timezone: null,
      now: guayaquil("2026-07-10"),
    }),
    null,
  );
});

test("a surface that never counts down is given no clock", () => {
  // The read-only card on an ended Event keeps the badge and the closing line
  // and never gets the countdown: omitting `now` is how it says so.
  assert.equal(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-12").toISOString() })),
    null,
  );
});

test("a clock that has run past the cutoff the server still calls open says nothing", () => {
  // The server owns the verdict. A viewer whose machine is a week fast can
  // produce a negative day count, and the honest answer to that is silence — a
  // rung invented from a clock we do not trust would be a claim nobody checked.
  assert.equal(
    ticketTypeBadge(open({ sales_cutoff_at: guayaquil("2026-07-01").toISOString() }), {
      timezone: ZONE,
      now: guayaquil("2026-07-10"),
    }),
    null,
  );
});

// --- The precedence table --------------------------------------------------

test("one Ticket Type wears one badge, ranked sold out then closed then limit", () => {
  // Every combination of the three states. Sold out beats closed because it is
  // the stronger fact and changes what the buyer does next; limit reached goes
  // last because it is a statement about one reader rather than about the Event
  // (ADR 0070). Both apps rank them identically, and a disagreement is a bug.
  const table: Array<[boolean, boolean, boolean, TicketTypeBadge]> = [
    [false, false, false, null],
    [false, false, true, { kind: "limit_reached" }],
    [false, true, false, { kind: "closed" }],
    [false, true, true, { kind: "closed" }],
    [true, false, false, { kind: "sold_out" }],
    [true, false, true, { kind: "sold_out" }],
    [true, true, false, { kind: "sold_out" }],
    [true, true, true, { kind: "sold_out" }],
  ];
  for (const [sold_out, closed, limitReached, expected] of table) {
    assert.deepEqual(
      ticketTypeBadge({ sold_out, closed, sales_cutoff_at: null }, { limitReached }),
      expected,
      `sold_out=${sold_out} closed=${closed} limitReached=${limitReached}`,
    );
  }
});

test("the countdown never shares a card with a state badge", () => {
  // No nagging about a deadline for a ticket that cannot be bought, and — the
  // part that matters — a skewed browser clock can never draw a countdown over
  // a card the server called closed, because the day count is only reached once
  // the card is known to be open.
  const soon = guayaquil("2026-07-12").toISOString();
  const now = guayaquil("2026-07-10");
  const within = { timezone: ZONE, now };
  assert.deepEqual(
    ticketTypeBadge({ sold_out: true, closed: false, sales_cutoff_at: soon }, within),
    { kind: "sold_out" },
  );
  assert.deepEqual(
    ticketTypeBadge({ sold_out: false, closed: true, sales_cutoff_at: soon }, within),
    { kind: "closed" },
  );
  assert.deepEqual(
    ticketTypeBadge(
      { sold_out: false, closed: false, sales_cutoff_at: soon },
      {
        ...within,
        limitReached: true,
      },
    ),
    { kind: "limit_reached" },
  );
});
