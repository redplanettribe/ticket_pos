import assert from "node:assert/strict";
import test from "node:test";

import {
  AFFILIATE_TRENDS_BUCKET_WIDTH,
  AFFILIATE_TRENDS_MIN_PLOT_WIDTH,
  ALL_PAGE_VIEWS_ID,
  affiliateTrendsPlotWidth,
  bucketDay,
  countYTicks,
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

test("the axis tops the tallest single value, rounded to a readable tick", () => {
  const datum = (values: Record<string, number>) => ({ key: "k", label: "l", values });
  // Separate lines never reach a stack's height: 7 and 3 top out at 8, not 10.
  assert.equal(multiSeriesYMax([datum({ a: 7, b: 3 })]), 8);
  assert.equal(multiSeriesYMax([datum({ a: 43 })]), 50);
  assert.equal(multiSeriesYMax([datum({ a: 0 })]), 1);
  assert.equal(multiSeriesYMax([]), 1);
});

test("the axis leaves little headroom: a day of 56 is drawn under 60, not 100", () => {
  const datum = (a: number) => ({ key: "k", label: "l", values: { a } });
  assert.equal(multiSeriesYMax([datum(56)]), 60);
  assert.equal(multiSeriesYMax([datum(39)]), 40);
  assert.equal(multiSeriesYMax([datum(17)]), 20);
  assert.equal(multiSeriesYMax([datum(7)]), 8);
  assert.equal(multiSeriesYMax([datum(1)]), 1);
  assert.equal(multiSeriesYMax([datum(61)]), 80);
  assert.equal(multiSeriesYMax([datum(81)]), 100);
  assert.equal(multiSeriesYMax([datum(250)]), 300);
});

test("every top the ladder can produce divides into whole, evenly spaced ticks", () => {
  const datum = (a: number) => ({ key: "k", label: "l", values: { a } });
  for (let tallest = 1; tallest <= 1200; tallest += 1) {
    const yMax = multiSeriesYMax([datum(tallest)]);
    assert.ok(yMax >= tallest, `${tallest} fits under ${yMax}`);
    // The old ladder drew a 56 under a 100; nothing is ever dwarfed like that now.
    assert.ok(yMax < 2 * tallest, `${tallest} is not dwarfed by ${yMax}`);
    const ticks = countYTicks(yMax);
    assert.equal(ticks[0], 0);
    assert.equal(ticks[ticks.length - 1], yMax);
    const step = ticks[1]! - ticks[0]!;
    for (const [index, tick] of ticks.entries()) {
      assert.ok(Number.isInteger(tick), `tick ${tick} under ${yMax} is whole`);
      assert.equal(tick, index * step, `ticks under ${yMax} are evenly spaced`);
    }
  }
});

test("the counting axis is divided in steps a reader counts in", () => {
  // Sales Trends' rule would label 80 in sixteens and 40 in eights; here the
  // division whose step starts with a 1, 2 or 5 wins.
  assert.deepEqual(countYTicks(80), [0, 20, 40, 60, 80]);
  assert.deepEqual(countYTicks(60), [0, 20, 40, 60]);
  assert.deepEqual(countYTicks(40), [0, 10, 20, 30, 40]);
  assert.deepEqual(countYTicks(30), [0, 10, 20, 30]);
  assert.deepEqual(countYTicks(20), [0, 5, 10, 15, 20]);
  assert.deepEqual(countYTicks(10), [0, 2, 4, 6, 8, 10]);
  assert.deepEqual(countYTicks(8), [0, 2, 4, 6, 8]);
  assert.deepEqual(countYTicks(6), [0, 2, 4, 6]);
  assert.deepEqual(countYTicks(3), [0, 1, 2, 3]);
  assert.deepEqual(countYTicks(1), [0, 1]);
  assert.deepEqual(countYTicks(0), [0]);
});

test("the plot grows with the span and never with the drawn series", () => {
  const week = affiliateTrendsPlotWidth(168);
  assert.equal(week, 168 * AFFILIATE_TRENDS_BUCKET_WIDTH);
  // A line is one point per series per bucket: a day of hours is the same
  // width with one link drawn as with seventeen.
  assert.equal(affiliateTrendsPlotWidth(24), 24 * AFFILIATE_TRENDS_BUCKET_WIDTH);
  assert.equal(affiliateTrendsPlotWidth(0), AFFILIATE_TRENDS_MIN_PLOT_WIDTH);
  assert.equal(affiliateTrendsPlotWidth(4, 900), 900);
  assert.equal(affiliateTrendsPlotWidth(4, 200), AFFILIATE_TRENDS_MIN_PLOT_WIDTH);
});

// --- the empty state ------------------------------------------------------

test("nothing counted yet is the empty state, not an all-zero chart", () => {
  assert.equal(hasPageViews(trendsWith([])), false);
  assert.equal(hasPageViews(trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 0 }])), false);
  assert.equal(hasPageViews(trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 1 }])), true);
});

// --- the Attributed Sales view (#415) ---------------------------------------

import {
  availableTrendsMetrics,
  hasTrendsData,
  salesSeries,
  type AffiliateSalesBucket,
} from "./affiliate-trends.ts";

function fullTrends(
  view_buckets: AffiliateViewBucket[],
  sales_buckets: AffiliateSalesBucket[] | null,
) {
  return { timezone: TZ, view_buckets, sales_buckets };
}

const SALE = (hour: string, link_id: string, sales: number, tickets: number, cents: number) => ({
  hour,
  link_id,
  sales,
  tickets,
  net_proceeds_cents: cents,
});

test("a civil-hour sales bucket lands in the UTC slot that IS that Event hour", () => {
  // 10:00 in Guayaquil (UTC-5) is 15:00 UTC — the window's last, still-filling slot.
  const data = salesSeries(
    fullTrends([], [SALE("2026-08-24T10:00", LINK_A, 2, 5, 12000)]),
    ORDER,
    "24h",
    NOW,
    "en",
  );
  assert.equal(data.length, 24);
  const last = data[23];
  assert.equal(last.key, "2026-08-24T15:00:00Z");
  assert.equal(last.values[LINK_A], 2);
  assert.deepEqual(last.details[LINK_A], { tickets: 5, netProceedsCents: 12000 });
});

test("quiet slots carry zero sales and zero figures for every drawn link", () => {
  const data = salesSeries(
    fullTrends([], [SALE("2026-08-24T10:00", LINK_A, 1, 1, 100)]),
    ORDER,
    "24h",
    NOW,
    "en",
  );
  const quiet = data[0];
  assert.deepEqual(quiet.values, { [LINK_A]: 0, [LINK_B]: 0 });
  assert.deepEqual(quiet.details[LINK_B], { tickets: 0, netProceedsCents: 0 });
});

test("the whole-page series has no place in the sales view", () => {
  const data = salesSeries(fullTrends([], []), ORDER, "24h", NOW, "en");
  assert.equal(Object.hasOwn(data[0].values, ALL_PAGE_VIEWS_ID), false);
});

test("a deselected link is absent from the sales datum, not zeroed", () => {
  const data = salesSeries(
    fullTrends([], [SALE("2026-08-24T10:00", LINK_A, 1, 2, 300)]),
    [LINK_B],
    "24h",
    NOW,
    "en",
  );
  assert.deepEqual(data[23].values, { [LINK_B]: 0 });
  assert.equal(Object.hasOwn(data[23].values, LINK_A), false);
});

test("daily ranges sum a day's sales figures on the Event's own calendar", () => {
  const data = salesSeries(
    fullTrends(
      [],
      [
        SALE("2026-08-23T21:00", LINK_A, 1, 2, 5000),
        SALE("2026-08-23T23:00", LINK_A, 2, 3, 7000),
        SALE("2026-08-24T09:00", LINK_A, 1, 1, 1000),
      ],
    ),
    ORDER,
    "30d",
    NOW,
    "en",
  );
  const byKey = new Map(data.map((datum) => [datum.key, datum]));
  assert.equal(byKey.get("2026-08-23")?.values[LINK_A], 3);
  assert.deepEqual(byKey.get("2026-08-23")?.details[LINK_A], {
    tickets: 5,
    netProceedsCents: 12000,
  });
  assert.equal(byKey.get("2026-08-24")?.values[LINK_A], 1);
});

test("all reaches back to sales older than any stored page view", () => {
  // Attributed Sales derive from the sales ledger, which predates the buckets'
  // launch — the axis must not silently truncate that history.
  const data = salesSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 2 }],
      [SALE("2026-08-20T12:00", LINK_A, 1, 1, 900)],
    ),
    ORDER,
    "all",
    NOW,
    "en",
  );
  assert.equal(data[0].key, "2026-08-20");
  assert.equal(data[0].values[LINK_A], 1);
  assert.equal(data.at(-1)?.key, "2026-08-24");
});

test("an externally registered Event has no sales series at all", () => {
  assert.deepEqual(salesSeries(fullTrends([], null), ORDER, "24h", NOW, "en"), []);
});

test("the metrics on offer follow the payload: no sales figures, no sales view", () => {
  assert.deepEqual(availableTrendsMetrics(fullTrends([], null)), ["clicks"]);
  assert.deepEqual(availableTrendsMetrics(fullTrends([], [])), ["clicks", "sales", "rate"]);
});

test("sales alone are enough to draw the surface", () => {
  assert.equal(hasTrendsData(fullTrends([], null)), false);
  assert.equal(hasTrendsData(fullTrends([], [])), false);
  assert.equal(hasTrendsData(fullTrends([], [SALE("2026-08-20T12:00", LINK_A, 1, 1, 1)])), true);
  assert.equal(
    hasTrendsData(fullTrends([{ hour: "2026-08-24T14:00:00Z", link_id: null, views: 1 }], null)),
    true,
  );
});

// --- Attribution Rate ------------------------------------------------------

// The Rate view divides two floors, so its tests speak in exact fractions: a
// quarter is 0.25, and a day with no clicks is null — a gap, never a zero and
// never an infinity.

import {
  RATE_TRENDS_RANGES,
  rateRange,
  rateSeries,
  rateYMax,
  rateYTicks,
} from "./affiliate-trends.ts";

test("the switcher offers the rate only where sales figures exist at all", () => {
  assert.deepEqual(availableTrendsMetrics({ sales_buckets: [] }), ["clicks", "sales", "rate"]);
  assert.deepEqual(availableTrendsMetrics({ sales_buckets: null }), ["clicks"]);
});

test("the rate view never offers 24h — its shortest honest range is a week", () => {
  assert.deepEqual([...RATE_TRENDS_RANGES], ["7d", "30d", "all"]);
  assert.equal(rateRange("24h"), "7d");
  assert.equal(rateRange("30d"), "30d");
});

test("the rate is drawn at days regardless of the range's own granularity", () => {
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 4 }],
      [{ hour: "2026-08-24T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 }],
    ),
    ORDER,
    "7d",
    NOW,
    "en",
    "daily",
  );
  assert.ok(data.length > 0);
  // Every key is a civil day, though "7d" draws the other views hourly.
  assert.ok(data.every((datum) => /^\d{4}-\d{2}-\d{2}$/.test(datum.key)));
  assert.equal(data[data.length - 1].key, "2026-08-24");
});

test("a day's rate is that day's sales over that day's clicks", () => {
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 4 }],
      [{ hour: "2026-08-24T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 }],
    ),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "daily",
  );
  const today = data[data.length - 1];
  assert.equal(today.values[LINK_A], 0.25);
  assert.deepEqual(today.details[LINK_A], { sales: 1, denominator: 4 });
});

test("a zero-click day yields no point — null, not zero and not Infinity", () => {
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 4 }],
      // A sale with no clicks that day — the attribution window at work.
      [{ hour: "2026-08-23T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 }],
    ),
    [LINK_A, LINK_B],
    "7d",
    NOW,
    "en",
    "daily",
  );
  const yesterday = data.find((datum) => datum.key === "2026-08-23");
  assert.ok(yesterday);
  assert.equal(yesterday.values[LINK_A], null);
  // A link with no history at all is a row of gaps, not a zero line.
  assert.ok(data.every((datum) => datum.values[LINK_B] === null));
});

test("the overall line divides every attributed sale by every page view", () => {
  const data = rateSeries(
    fullTrends(
      [
        { hour: "2026-08-24T14:00:00Z", link_id: null, views: 10 },
        { hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 4 },
      ],
      [
        { hour: "2026-08-24T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 },
        { hour: "2026-08-24T10:00", link_id: LINK_B, sales: 1, tickets: 2, net_proceeds_cents: 200 },
      ],
    ),
    ORDER,
    "7d",
    NOW,
    "en",
    "daily",
  );
  const today = data[data.length - 1];
  assert.equal(today.values[ALL_PAGE_VIEWS_ID], 0.2);
  assert.deepEqual(today.details[ALL_PAGE_VIEWS_ID], { sales: 2, denominator: 10 });
});

test("the cumulative rate is all sales to date over all clicks to date, even from before the window", () => {
  const data = rateSeries(
    fullTrends(
      // Both the clicks and the sale predate a 7-day window ending at NOW.
      [{ hour: "2026-08-10T14:00:00Z", link_id: LINK_A, views: 4 }],
      [{ hour: "2026-08-10T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 }],
    ),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  // Every window day carries the history: a quiet week is a flat line at 25%.
  assert.ok(data.length > 0);
  assert.ok(data.every((datum) => datum.values[LINK_A] === 0.25));
  assert.deepEqual(data[data.length - 1].details[LINK_A], { sales: 1, denominator: 4 });
});

test("the cumulative line starts at the first click, not before it", () => {
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 2 }],
      [{ hour: "2026-08-24T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 }],
    ),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  const before = data.filter((datum) => datum.key < "2026-08-24");
  assert.ok(before.length > 0);
  assert.ok(before.every((datum) => datum.values[LINK_A] === null));
  assert.equal(data[data.length - 1].values[LINK_A], 0.5);
});

test("a Reversal that left the payload lowers the past points", () => {
  const clicks: AffiliateViewBucket[] = [
    { hour: "2026-08-20T14:00:00Z", link_id: LINK_A, views: 4 },
  ];
  const before = rateSeries(
    fullTrends(clicks, [
      { hour: "2026-08-20T09:00", link_id: LINK_A, sales: 2, tickets: 2, net_proceeds_cents: 200 },
    ]),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  const after = rateSeries(
    fullTrends(clicks, [
      { hour: "2026-08-20T09:00", link_id: LINK_A, sales: 1, tickets: 1, net_proceeds_cents: 100 },
    ]),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  assert.equal(before[before.length - 1].values[LINK_A], 0.5);
  assert.equal(after[after.length - 1].values[LINK_A], 0.25);
});

test("an externally registered Event has no rate to draw", () => {
  assert.deepEqual(rateSeries(fullTrends([], null), ORDER, "7d", NOW, "en", "cumulative"), []);
});

test("the rate axis rounds up to a readable fraction and never collapses", () => {
  const datum = (value: number | null) => ({
    key: "2026-08-24",
    label: "Aug 24",
    values: { [LINK_A]: value },
    details: { [LINK_A]: { sales: 0, denominator: 0 } },
  });
  assert.equal(rateYMax([datum(0.034)]), 0.04);
  // 5.2% climbs to 6%, not 10%: the same headroom rule as the counting views.
  assert.equal(rateYMax([datum(0.052)]), 0.06);
  assert.equal(rateYMax([datum(0.05)]), 0.05);
  // 3 × 0.1 is not 0.3 in floating point; the top is settled to basis points.
  assert.equal(rateYMax([datum(0.25)]), 0.3);
  assert.equal(rateYMax([datum(null)]), 0.05);
  assert.equal(rateYMax([]), 0.05);
  const ticks = rateYTicks(0.05);
  assert.equal(ticks[0], 0);
  assert.equal(ticks[ticks.length - 1], 0.05);
  assert.equal(ticks.length, 6);
});

test("the rate axis is divided in whole tenths of a percent, the finest label it draws", () => {
  assert.deepEqual(rateYTicks(0.06), [0, 0.02, 0.04, 0.06]);
  // 0.6% in five is 0.12% a step, which would be labelled 0.1%: in three instead.
  assert.deepEqual(rateYTicks(0.006), [0, 0.002, 0.004, 0.006]);
  assert.deepEqual(rateYTicks(0.002), [0, 0.001, 0.002]);
  // Too small to divide into tenths at all: the ends alone.
  assert.deepEqual(rateYTicks(0.001), [0, 0.001]);
});

test("a bucket on a civil day after the reader's own moves no rate — skew waits for its day", () => {
  const data = rateSeries(
    fullTrends(
      [
        { hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 4 },
        // Clock skew: an API bucket on tomorrow's civil day. The other views
        // have no slot for it; the cumulative tally must not count it either.
        { hour: "2026-08-25T06:00:00Z", link_id: LINK_A, views: 100 },
      ],
      [
        SALE("2026-08-24T09:00", LINK_A, 1, 1, 100),
        SALE("2026-08-25T09:00", LINK_A, 5, 5, 500),
      ],
    ),
    [LINK_A],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  const today = data[data.length - 1];
  assert.equal(today.key, "2026-08-24");
  assert.equal(today.values[LINK_A], 0.25);
  assert.deepEqual(today.details[LINK_A], { sales: 1, denominator: 4 });
});

test("the cumulative rate begins where the page views do — a sale before the first counted view is not divided by launch day's clicks", () => {
  // The dev event at launch: a lifetime of 840 sales in the ledger, and one
  // page view counted so far. 840 ÷ 1 is not a rate.
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 1 }],
      [
        SALE("2026-05-01T09:00", LINK_A, 840, 840, 84000),
        SALE("2026-08-24T09:00", LINK_A, 1, 1, 100),
      ],
    ),
    [LINK_A, ALL_PAGE_VIEWS_ID],
    "7d",
    NOW,
    "en",
    "cumulative",
  );
  const today = data[data.length - 1];
  assert.equal(today.values[LINK_A], 1);
  assert.deepEqual(today.details[LINK_A], { sales: 1, denominator: 1 });
  // The overall line has no page-view bucket to divide by, so no rate — not
  // 841 over nothing.
  assert.equal(today.values[ALL_PAGE_VIEWS_ID], null);
  assert.deepEqual(today.details[ALL_PAGE_VIEWS_ID], { sales: 1, denominator: 0 });
});

test("all on the rate view starts at the first counted page view, not at the first sale", () => {
  const data = rateSeries(
    fullTrends(
      [{ hour: "2026-08-20T14:00:00Z", link_id: LINK_A, views: 4 }],
      [
        SALE("2026-05-01T09:00", LINK_A, 840, 840, 84000),
        SALE("2026-08-22T09:00", LINK_A, 1, 1, 100),
      ],
    ),
    [LINK_A],
    "all",
    NOW,
    "en",
    "cumulative",
  );
  assert.equal(data[0].key, "2026-08-20");
  assert.equal(data[data.length - 1].key, "2026-08-24");
  assert.equal(data[data.length - 1].values[LINK_A], 0.25);
});

test("with no page views ever counted, the rate view has nothing to draw on all", () => {
  const data = rateSeries(
    fullTrends([], [SALE("2026-08-22T09:00", LINK_A, 1, 1, 100)]),
    [LINK_A],
    "all",
    NOW,
    "en",
    "cumulative",
  );
  assert.deepEqual(data, []);
});

// --- the Hourly/Daily toggle -----------------------------------------------

import {
  AFFILIATE_TRENDS_GRANULARITIES,
  granularityChoosable,
  latestBucketWithData,
  latestBucketsScrollLeft,
  trendsGranularity,
} from "./affiliate-trends.ts";

test("the toggle offers hourly and daily, and 24h is always hourly", () => {
  assert.deepEqual([...AFFILIATE_TRENDS_GRANULARITIES], ["hour", "day"]);
  assert.equal(trendsGranularity("24h", "day"), "hour");
  assert.equal(trendsGranularity("7d", "day"), "day");
  assert.equal(trendsGranularity("30d", "hour"), "hour");
  assert.equal(granularityChoosable("24h"), false);
  assert.equal(granularityChoosable("7d"), true);
  assert.equal(granularityChoosable("all"), true);
});

test("a week can be read by the day", () => {
  const data = viewsSeries(
    trendsWith([
      { hour: "2026-08-24T14:00:00Z", link_id: null, views: 3 },
      { hour: "2026-08-24T02:00:00Z", link_id: null, views: 2 },
    ]),
    [ALL_PAGE_VIEWS_ID],
    "7d",
    NOW,
    "en",
    "day",
  );
  assert.ok(data.every((datum) => /^\d{4}-\d{2}-\d{2}$/.test(datum.key)));
  assert.equal(data.length, 8);
  // 02:00Z is the 23rd in Guayaquil; 14:00Z is the 24th.
  assert.equal(data[data.length - 1].values[ALL_PAGE_VIEWS_ID], 3);
  assert.equal(data[data.length - 2].values[ALL_PAGE_VIEWS_ID], 2);
});

test("a month can be read by the hour, sales included", () => {
  const views = viewsSeries(
    trendsWith([{ hour: "2026-08-24T14:00:00Z", link_id: LINK_A, views: 3 }]),
    [LINK_A],
    "30d",
    NOW,
    "en",
    "hour",
  );
  assert.equal(views.length, 30 * 24);
  assert.equal(views[views.length - 2].key, "2026-08-24T14:00:00Z");
  assert.equal(views[views.length - 2].values[LINK_A], 3);

  const sales = salesSeries(
    fullTrends([], [SALE("2026-08-24T09:00", LINK_A, 2, 2, 200)]),
    [LINK_A],
    "30d",
    NOW,
    "en",
    "hour",
  );
  assert.equal(sales.length, 30 * 24);
  assert.equal(sales[sales.length - 2].values[LINK_A], 2);
});

// --- opening on the newest bucket with data ---------------------------------

test("the newest bucket with data is found from the end, and an empty window has none", () => {
  const data: { values: Record<string, number | null> }[] = [
    { values: { a: 0, b: 2 } },
    { values: { a: 1, b: 0 } },
    { values: { a: 0, b: 0 } },
    { values: { a: null } },
  ];
  assert.equal(latestBucketWithData(data), 1);
  assert.equal(latestBucketWithData([{ values: { a: 0 } }, { values: { a: null } }]), -1);
  assert.equal(latestBucketWithData([]), -1);
});

test("the window opens with the newest bucket with data at its right edge", () => {
  // 10 buckets of 100px after a 64px axis: bucket 4's right edge is at 564.
  const geometry = { plotWidth: 1000, plotLeft: 64, plotRight: 8, clientWidth: 400, scrollWidth: 1072 };
  const data = Array.from({ length: 10 }, (_, index) => ({ values: { a: index === 4 ? 1 : 0 } }));
  assert.equal(latestBucketsScrollLeft(data, geometry), 564 + 8 - 400);
});

test("an empty window opens at the plot's end, and the anchor never overshoots either edge", () => {
  const geometry = { plotWidth: 1000, plotLeft: 64, plotRight: 8, clientWidth: 400, scrollWidth: 1072 };
  const empty = Array.from({ length: 10 }, () => ({ values: { a: 0 } }));
  assert.equal(latestBucketsScrollLeft(empty, geometry), 672);
  const first = Array.from({ length: 10 }, (_, index) => ({ values: { a: index === 0 ? 1 : 0 } }));
  assert.equal(latestBucketsScrollLeft(first, geometry), 0);
  const last = Array.from({ length: 10 }, (_, index) => ({ values: { a: index === 9 ? 1 : 0 } }));
  assert.equal(latestBucketsScrollLeft(last, geometry), 672);
});

// --- listing: only what counted in the window, busiest first (#431) ---------

// The listing is a pure derivation over the builders above: which chips the
// legend shows, in what order, and whether the window has anything at all.
// `selected` is an input it never rewrites — a dimmed link stays dimmed
// through every range and metric switch, listed or not.

import {
  allSeriesAction,
  flipAllSeries,
  listTrendsSeries,
  toggleListedSeries,
} from "./affiliate-trends.ts";

const LINK_C = "5f0f8f6a-0000-0000-0000-00000000000c";
const LINKS = [LINK_A, LINK_B, LINK_C].map((id) => ({ id, name: id.slice(-1), active: true }));
const ALL_SELECTED = [ALL_PAGE_VIEWS_ID, LINK_A, LINK_B, LINK_C];

function listedTrends(
  view_buckets: AffiliateViewBucket[],
  sales_buckets: AffiliateSalesBucket[] | null = [],
) {
  return { timezone: TZ, links: LINKS, view_buckets, sales_buckets };
}

const VIEW = (hour: string, link_id: string | null, views: number) => ({ hour, link_id, views });

test("clicks view: a link with no Clicks in the window is unlisted and undrawn", () => {
  const trends = listedTrends([
    VIEW("2026-08-24T14:00:00Z", null, 5),
    VIEW("2026-08-24T14:00:00Z", LINK_A, 2),
    // B clicked three days ago — outside 24h, inside 7d.
    VIEW("2026-08-21T14:00:00Z", LINK_B, 1),
  ]);
  const day = listTrendsSeries(trends, ALL_SELECTED, "clicks", "24h", "hour", "cumulative", NOW);
  assert.deepEqual(day.listed, [ALL_PAGE_VIEWS_ID, LINK_A]);
  assert.deepEqual(day.drawn, [ALL_PAGE_VIEWS_ID, LINK_A]);
  assert.equal(day.empty, false);
});

test("clicks view: widening the range lists the link again with its selection as the reader left it", () => {
  const trends = listedTrends([
    VIEW("2026-08-24T14:00:00Z", LINK_A, 2),
    VIEW("2026-08-21T14:00:00Z", LINK_B, 1),
  ]);
  // B was dimmed before it went unlisted on 24h; on 7d it is listed, still dimmed.
  const selected = [ALL_PAGE_VIEWS_ID, LINK_A, LINK_C];
  const week = listTrendsSeries(trends, selected, "clicks", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(week.listed, [LINK_A, LINK_B]);
  assert.deepEqual(week.drawn, [LINK_A]);
  // The selection is the input, never the output: the unlisted C is still in it.
  assert.deepEqual(selected, [ALL_PAGE_VIEWS_ID, LINK_A, LINK_C]);
});

test("clicks view: the whole page is listed only when it received a view in the window", () => {
  const trends = listedTrends([
    VIEW("2026-08-21T14:00:00Z", null, 5),
    VIEW("2026-08-24T14:00:00Z", LINK_A, 2),
  ]);
  const day = listTrendsSeries(trends, ALL_SELECTED, "clicks", "24h", "hour", "cumulative", NOW);
  assert.deepEqual(day.listed, [LINK_A]);
  const week = listTrendsSeries(trends, ALL_SELECTED, "clicks", "7d", "day", "cumulative", NOW);
  assert.deepEqual(week.listed, [ALL_PAGE_VIEWS_ID, LINK_A]);
});

test("sales view: only links with an Attributed Sale in the window, and never the whole page", () => {
  const trends = listedTrends(
    [VIEW("2026-08-24T14:00:00Z", null, 50), VIEW("2026-08-24T14:00:00Z", LINK_A, 9)],
    [SALE("2026-08-24T09:00", LINK_B, 1, 1, 100), SALE("2026-08-20T09:00", LINK_C, 3, 3, 300)],
  );
  const day = listTrendsSeries(trends, ALL_SELECTED, "sales", "24h", "hour", "cumulative", NOW);
  assert.deepEqual(day.listed, [LINK_B]);
  const week = listTrendsSeries(trends, ALL_SELECTED, "sales", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(week.listed, [LINK_C, LINK_B]);
});

test("rate view: a link with Clicks and no sales is listed at 0%, a link with no Clicks is not", () => {
  const trends = listedTrends(
    [VIEW("2026-08-24T14:00:00Z", null, 10), VIEW("2026-08-24T14:00:00Z", LINK_A, 4)],
    [SALE("2026-08-24T09:00", LINK_B, 1, 1, 100)],
  );
  for (const view of ["daily", "cumulative"] as const) {
    const listed = listTrendsSeries(trends, ALL_SELECTED, "rate", "7d", "hour", view, NOW);
    assert.deepEqual(listed.listed, [ALL_PAGE_VIEWS_ID, LINK_A], view);
  }
});

test("rate view: Cumulative lists a link whose Clicks predate the window; Daily does not", () => {
  const trends = listedTrends(
    [VIEW("2026-08-24T14:00:00Z", null, 10), VIEW("2026-08-10T14:00:00Z", LINK_A, 4)],
    [SALE("2026-08-10T09:00", LINK_A, 1, 1, 100)],
  );
  const cumulative = listTrendsSeries(trends, ALL_SELECTED, "rate", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(cumulative.listed, [ALL_PAGE_VIEWS_ID, LINK_A]);
  const daily = listTrendsSeries(trends, ALL_SELECTED, "rate", "7d", "hour", "daily", NOW);
  assert.deepEqual(daily.listed, [ALL_PAGE_VIEWS_ID]);
});

test("the whole page is pinned first, then links by window total, ties in API order", () => {
  const trends = listedTrends([
    VIEW("2026-08-24T14:00:00Z", null, 1),
    VIEW("2026-08-24T14:00:00Z", LINK_A, 2),
    VIEW("2026-08-24T13:00:00Z", LINK_B, 3),
    VIEW("2026-08-24T12:00:00Z", LINK_B, 3),
    VIEW("2026-08-24T14:00:00Z", LINK_C, 2),
  ]);
  const day = listTrendsSeries(trends, ALL_SELECTED, "clicks", "24h", "hour", "cumulative", NOW);
  assert.deepEqual(day.listed, [ALL_PAGE_VIEWS_ID, LINK_B, LINK_A, LINK_C]);
  // The whole page leads however small: it is the page, not a competitor.
  const sales = listTrendsSeries(
    listedTrends([], [SALE("2026-08-24T09:00", LINK_C, 2, 2, 200), SALE("2026-08-24T09:00", LINK_A, 1, 1, 100)]),
    ALL_SELECTED,
    "sales",
    "24h",
    "hour",
    "cumulative",
    NOW,
  );
  assert.deepEqual(sales.listed, [LINK_C, LINK_A]);
});

test("a window in which nothing was counted is empty, whichever series are selected", () => {
  const trends = listedTrends(
    [VIEW("2026-08-21T14:00:00Z", null, 5), VIEW("2026-08-21T14:00:00Z", LINK_A, 2)],
    [SALE("2026-08-21T09:00", LINK_A, 1, 1, 100)],
  );
  const clicks = listTrendsSeries(trends, [LINK_B], "clicks", "24h", "hour", "cumulative", NOW);
  assert.deepEqual(clicks, { listed: [], drawn: [], empty: true, blank: null });
  const sales = listTrendsSeries(trends, ALL_SELECTED, "sales", "24h", "hour", "cumulative", NOW);
  assert.equal(sales.empty, true);
  // The same data is not empty on the week — the window, not the history, is empty.
  const week = listTrendsSeries(trends, ALL_SELECTED, "clicks", "7d", "hour", "cumulative", NOW);
  assert.equal(week.empty, false);
  // A selection that meets nothing listed draws nothing: the reader picked a
  // link on one window, and this window lists only others. The chart stays
  // empty and says why, and `selected` is not rewritten for it.
  const chosen = [LINK_B];
  const elsewhere = listTrendsSeries(trends, chosen, "clicks", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(elsewhere, {
    listed: [ALL_PAGE_VIEWS_ID, LINK_A],
    drawn: [],
    empty: false,
    blank: "unlisted",
  });
  assert.deepEqual(chosen, [LINK_B]);
  // With nothing selected at all, nothing is drawn, and the reader is asked to pick.
  const cleared = listTrendsSeries(trends, [], "clicks", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(cleared, {
    listed: [ALL_PAGE_VIEWS_ID, LINK_A],
    drawn: [],
    empty: false,
    blank: "cleared",
  });
  // Once one listed series is chosen, only the chosen are drawn.
  const one = listTrendsSeries(trends, [LINK_A, LINK_B], "clicks", "7d", "hour", "cumulative", NOW);
  assert.deepEqual(one.drawn, [LINK_A]);
});

test("a chip toggles freely, the last listed one included, in API order", () => {
  const order = [ALL_PAGE_VIEWS_ID, LINK_A, LINK_B];
  // Switching off the last chip on is allowed: the chart goes blank, not refused.
  assert.deepEqual(toggleListedSeries(order, [LINK_A], LINK_A), []);
  // Other picks, listed in this window or not, are left alone.
  assert.deepEqual(toggleListedSeries(order, [LINK_A, LINK_B], LINK_A), [LINK_B]);
  // Toggling a chip back on keeps API order.
  assert.deepEqual(toggleListedSeries(order, [LINK_B], LINK_A), [LINK_A, LINK_B]);
});

test("the select-all pill clears everything while a line is drawn, and selects everything while none is", () => {
  const order = [ALL_PAGE_VIEWS_ID, LINK_A, LINK_B];
  // Something drawn: Deselect all, including a pick this window does not list.
  assert.equal(allSeriesAction([LINK_A]), "deselect");
  assert.deepEqual(flipAllSeries(order, [LINK_A]), []);
  // Nothing drawn, whether cleared or picked elsewhere: Select all, back to the opening state.
  assert.equal(allSeriesAction([]), "select");
  assert.deepEqual(flipAllSeries(order, []), [ALL_PAGE_VIEWS_ID, LINK_A, LINK_B]);
});
