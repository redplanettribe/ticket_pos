/**
 * The date windows behind the explorer's preset chips.
 *
 * whenToRange is the whole of "This weekend" as far as a Customer is concerned:
 * the chip lights up either way, the query runs either way, and a wrong window
 * shows a plausible-looking grid of the wrong Events. Nothing downstream can
 * catch that, so the boundaries are asserted here — against a fixed `now`, which
 * is why the function takes one.
 */

import assert from "node:assert/strict";
import test from "node:test";

// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import { WHEN_PRESETS, isWhenPreset, whenToRange } from "./when.ts";

/** A local-time Date, so the assertions read in the same frame the code works in. */
function local(y: number, m: number, d: number, h = 12, min = 0): Date {
  return new Date(y, m - 1, d, h, min, 0, 0);
}

/** The local wall-clock reading of an RFC3339 instant, for legible assertions. */
function wall(iso: string | undefined): string {
  assert.ok(iso, "expected a bound");
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

test("'all' is an absent window rather than a wide one", () => {
  // The chip must not narrow the query at all: an unbounded range would still
  // exclude Events outside whatever bounds it picked.
  assert.deepEqual(whenToRange("all"), {});
});

test("'week' and 'month' run from now to the horizon", () => {
  const now = local(2026, 7, 31, 9, 30); // a Friday

  const week = whenToRange("week", now);
  assert.equal(wall(week.from), "2026-07-31 09:30:00");
  assert.equal(wall(week.to), "2026-08-07 09:30:00");

  const month = whenToRange("month", now);
  assert.equal(wall(month.from), "2026-07-31 09:30:00");
  assert.equal(wall(month.to), "2026-08-31 09:30:00");
});

test("'weekend' from a weekday is the coming Sat 00:00 - Sun 23:59:59", () => {
  const range = whenToRange("weekend", local(2026, 7, 29, 15, 0)); // Wednesday
  assert.equal(wall(range.from), "2026-08-01 00:00:00");
  assert.equal(wall(range.to), "2026-08-02 23:59:59");
});

test("'weekend' from Saturday afternoon starts at now, not at midnight past", () => {
  // Events that already started this morning are over; the window opens here.
  const range = whenToRange("weekend", local(2026, 8, 1, 15, 0)); // Saturday
  assert.equal(wall(range.from), "2026-08-01 15:00:00");
  assert.equal(wall(range.to), "2026-08-02 23:59:59");
});

test("'weekend' on Sunday stays in the weekend in progress", () => {
  // The regression: a Sunday's Saturday is *yesterday*. Wrapping it forward
  // sent Sunday browsers to next weekend and hid everything happening today.
  const range = whenToRange("weekend", local(2026, 8, 2, 11, 0)); // Sunday
  assert.equal(wall(range.from), "2026-08-02 11:00:00");
  assert.equal(wall(range.to), "2026-08-02 23:59:59");
});

test("'weekend' late on Sunday still offers the rest of the night", () => {
  const range = whenToRange("weekend", local(2026, 8, 2, 22, 30));
  assert.equal(wall(range.from), "2026-08-02 22:30:00");
  assert.equal(wall(range.to), "2026-08-02 23:59:59");
});

test("every weekend window is non-empty and ends on a Sunday night", () => {
  // Swept across a full week so no starting day can produce an inverted or
  // empty range - the shape the SQL turns into a silently blank grid.
  for (let day = 26; day <= 32; day += 1) {
    const now = local(2026, 7, day, 13, 0);
    const range = whenToRange("weekend", now);
    const from = new Date(range.from!);
    const to = new Date(range.to!);
    assert.ok(from < to, `inverted window from ${now.toDateString()}`);
    assert.ok(from >= now, `window opens in the past from ${now.toDateString()}`);
    assert.equal(to.getDay(), 0, `window must end on a Sunday from ${now.toDateString()}`);
    assert.equal(to.getHours(), 23);
  }
});

test("isWhenPreset accepts exactly the presets the chip bar offers", () => {
  // The guard is what keeps an arbitrary ?when= out of the range computation.
  for (const preset of WHEN_PRESETS) {
    assert.ok(isWhenPreset(preset), `${preset} is offered but rejected`);
  }
  assert.equal(isWhenPreset(undefined), false);
  assert.equal(isWhenPreset("tomorrow"), false);
  assert.equal(isWhenPreset("WEEK"), false);
});
