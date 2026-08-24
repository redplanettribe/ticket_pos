import assert from "node:assert/strict";
import test from "node:test";

import {
  AFFILIATE_TRENDS_MIN_PLOT_WIDTH,
  ALL_PAGE_VIEWS_ID,
  affiliateTrendsPlotWidth,
  bucketDay,
  hasPageViews,
  multiSeriesYMax,
  rangeGranularity,
  toggleSeriesSelection,
  truncateToUTCHour,
  utcHourKey,
  viewsSeries,
  type AffiliateViewBucket,
} from "./affiliate-trends.ts";

// A fixed reading moment, mid-hour on purpose: the current, still-filling hour
// must be the window's last slot. The Event sits in Guayaquil (UTC-5, no DST),
// so its civil days genuinely differ from UTC days in the small hours.
const NOW = new Date("2026-08-24T15:30:00Z");
const TZ = "America/Guayaquil";

const LINK_A = "5f0f8f6a-0000-0000-0000-00000000000a";
const LINK_B = "5f0f8f6a-0000-0000-0000-00000000000b";
const ORDER = [ALL_PAGE_VIEWS_ID, LINK_A, LINK_B];

function trendsWith(view_buckets: AffiliateViewBucket[]) {
  return { timezone: TZ, view_buckets };
}

// --- range to granularity -------------------------------------------------

test("the short ranges are hourly and the long ones daily, with no choice", () => {
  assert.equal(rangeGranularity("24h"), "hour");
  assert.equal(rangeGranularity("7d"), "hour");
  assert.equal(rangeGranularity("30d"), "day");
  assert.equal(rangeGranularity("all"), "day");
});

// --- hour keys ------------------------------------------------------------

test("an instant is filed under its UTC hour", () => {
  assert.equal(utcHourKey(new Date("2026-08-24T15:30:59.500Z")), "2026-08-24T15:00:00Z");
  assert.equal(truncateToUTCHour(NOW).toISOString(), "2026-08-24T15:00:00.000Z");
});

test("a UTC hour lands on the Event's civil day, not UTC's", () => {
  // 03:00 UTC is 22:00 the previous evening in Guayaquil.
  assert.equal(bucketDay("2026-08-24T03:00:00Z", TZ), "2026-08-23");
  assert.equal(bucketDay("2026-08-24T14:00:00Z", TZ), "2026-08-24");
});

// --- hourly windowing and zero-fill ---------------------------------------

test("24h is exactly 24 hourly slots ending on the current hour", () => {
  const data = viewsSeries(trendsWith([]), ORDER, "24h", NOW, "en");
  assert.equal(data.length, 24);
  assert.equal(data[0].key, "2026-08-23T16:00:00Z");
  assert.equal(data[23].key, "2026-08-24T15:00:00Z");
});

test("a quiet hour keeps its slot with zeros for every drawn series", () => {
  const data = viewsSeries(
    trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 3 }]),
    ORDER,
    "24h",
    NOW,
    "en",
  );
  const quiet = data[0];
  assert.deepEqual(quiet.values, { [ALL_PAGE_VIEWS_ID]: 0, [LINK_A]: 0, [LINK_B]: 0 });
});

test("stored buckets land in their own hour, split by series", () => {
  const data = viewsSeries(
    trendsWith([
      { hour: "2026-08-24T15:00:00Z", link_id: null, views: 5 },
      { hour: "2026-08-24T15:00:00Z", link_id: LINK_A, views: 2 },
      { hour: "2026-08-24T14:00:00Z", link_id: null, views: 3 },
    ]),
    ORDER,
    "24h",
    NOW,
    "en",
  );
  const last = data[23];
  assert.deepEqual(last.values, { [ALL_PAGE_VIEWS_ID]: 5, [LINK_A]: 2, [LINK_B]: 0 });
  assert.equal(data[22].values[ALL_PAGE_VIEWS_ID], 3);
  // Views across an hour boundary are two slots, never one.
  assert.notEqual(data[22].key, data[23].key);
});

test("a bucket older than the window is left out of it", () => {
  const data = viewsSeries(
    trendsWith([{ hour: "2026-08-23T15:00:00Z", link_id: null, views: 9 }]),
    ORDER,
    "24h",
    NOW,
    "en",
  );
  assert.ok(data.every((datum) => datum.values[ALL_PAGE_VIEWS_ID] === 0));
});

test("a deselected series is absent from the datum, not zeroed", () => {
  const data = viewsSeries(
    trendsWith([
      { hour: "2026-08-24T15:00:00Z", link_id: null, views: 5 },
      { hour: "2026-08-24T15:00:00Z", link_id: LINK_A, views: 2 },
    ]),
    [LINK_A],
    "24h",
    NOW,
    "en",
  );
  assert.deepEqual(data[23].values, { [LINK_A]: 2 });
  assert.equal(Object.hasOwn(data[23].values, ALL_PAGE_VIEWS_ID), false);
});

test("7d is a week of hourly slots", () => {
  const data = viewsSeries(trendsWith([]), ORDER, "7d", NOW, "en");
  assert.equal(data.length, 7 * 24);
  assert.equal(data.at(-1)?.key, "2026-08-24T15:00:00Z");
});

// --- daily aggregation ----------------------------------------------------

test("daily ranges sum a day's hours on the Event's own calendar", () => {
  const data = viewsSeries(
    trendsWith([
      // Both small-hours UTC buckets belong to the Guayaquil evening of the 23rd.
      { hour: "2026-08-24T02:00:00Z", link_id: null, views: 1 },
      { hour: "2026-08-24T03:00:00Z", link_id: null, views: 4 },
      { hour: "2026-08-24T14:00:00Z", link_id: null, views: 7 },
    ]),
    ORDER,
    "30d",
    NOW,
    "en",
  );
  const byKey = new Map(data.map((datum) => [datum.key, datum]));
  assert.equal(byKey.get("2026-08-23")?.values[ALL_PAGE_VIEWS_ID], 5);
  assert.equal(byKey.get("2026-08-24")?.values[ALL_PAGE_VIEWS_ID], 7);
});

test("30d spans 31 Event-timezone days ending today, each day one slot", () => {
  const data = viewsSeries(trendsWith([]), ORDER, "30d", NOW, "en");
  assert.equal(data.length, 31);
  assert.equal(data[0].key, "2026-07-25");
  assert.equal(data.at(-1)?.key, "2026-08-24");
  assert.equal(new Set(data.map((datum) => datum.key)).size, data.length);
});

// --- the "all" range ------------------------------------------------------

test("all starts at the earliest stored bucket", () => {
  const data = viewsSeries(
    trendsWith([
      { hour: "2026-08-22T10:00:00Z", link_id: LINK_A, views: 1 },
      { hour: "2026-08-24T14:00:00Z", link_id: null, views: 2 },
    ]),
    ORDER,
    "all",
    NOW,
    "en",
  );
  assert.equal(data[0].key, "2026-08-22");
  assert.equal(data.at(-1)?.key, "2026-08-24");
  assert.equal(data.length, 3);
});

test("all with nothing stored draws nothing rather than an invented span", () => {
  assert.deepEqual(viewsSeries(trendsWith([]), ORDER, "all", NOW, "en"), []);
});

// --- labels ---------------------------------------------------------------

// Structural only: the exact spelling belongs to Intl and differs between ICU
// builds, but every slot must carry SOME label for the axis to draw.
test("every slot carries a non-empty label", () => {
  for (const range of ["24h", "30d"] as const) {
    const data = viewsSeries(
      trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 1 }]),
      ORDER,
      range,
      NOW,
      "en",
    );
    assert.ok(data.every((datum) => datum.label.length > 0));
  }
});

// --- legend selection -----------------------------------------------------

test("toggling a drawn series off removes it; toggling back restores order", () => {
  const without = toggleSeriesSelection(ORDER, ORDER, LINK_A);
  assert.deepEqual(without, [ALL_PAGE_VIEWS_ID, LINK_B]);
  assert.deepEqual(toggleSeriesSelection(ORDER, without, LINK_A), ORDER);
});

test("the last drawn series cannot be deselected", () => {
  assert.deepEqual(toggleSeriesSelection(ORDER, [LINK_B], LINK_B), [LINK_B]);
});

// --- axis scale and plot width --------------------------------------------

test("the axis tops the tallest single bar, rounded to a readable tick", () => {
  const datum = (values: Record<string, number>) => ({ key: "k", label: "l", values });
  assert.equal(multiSeriesYMax([datum({ a: 7, b: 3 })]), 10);
  assert.equal(multiSeriesYMax([datum({ a: 43 })]), 50);
  // Grouped bars never reach a stack's height: 7 and 3 top out at 10, not 20.
  assert.equal(multiSeriesYMax([datum({ a: 0 })]), 1);
  assert.equal(multiSeriesYMax([]), 1);
});

test("the plot grows with the span and with the drawn series", () => {
  const week = affiliateTrendsPlotWidth(168, 3);
  assert.equal(week, 168 * 30);
  assert.ok(affiliateTrendsPlotWidth(168, 1) < week);
  assert.equal(affiliateTrendsPlotWidth(0, 3), AFFILIATE_TRENDS_MIN_PLOT_WIDTH);
  assert.equal(affiliateTrendsPlotWidth(4, 1, 900), 900);
});

// --- the empty state ------------------------------------------------------

test("nothing counted yet is the empty state, not an all-zero chart", () => {
  assert.equal(hasPageViews(trendsWith([])), false);
  assert.equal(hasPageViews(trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 0 }])), false);
  assert.equal(hasPageViews(trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 1 }])), true);
});
