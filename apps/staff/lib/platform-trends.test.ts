import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_PLATFORM_TRENDS_VIEW,
  PLATFORM_TRENDS_API_PATH,
  STACK_COMPARISON_ID,
  activityChartData,
  activityTile,
  allBeforeMarker,
  comparisonReachesSalesOnly,
  comparisonSeriesId,
  countTile,
  formatTrendBucket,
  formatTrendChange,
  formatTrendMean,
  markerBucketKey,
  moneyTile,
  parsePlatformTrendsParams,
  platformTrendsQuery,
  platformTrendsRequestPath,
  platformTrendsScrollAnchor,
  platformTrendsViewAfterComparisonChange,
  platformTrendsViewAfterRangeChange,
  platformTrendsViewAfterSpanChange,
  salesOnlyThroughout,
  seriesChartData,
  spanBuckets,
  stackedChartData,
  trendChartYMax,
  type PlatformTrendActivityFigure,
  type PlatformTrendBucket,
  type PlatformTrendFigure,
  type PlatformTrendSeries,
  type PlatformTrends,
} from "./platform-trends.ts";

// ---------------------------------------------------------------------------
// Address state to request
// ---------------------------------------------------------------------------

test("a bare address is the default reading: 30 days against the span before", () => {
  assert.deepEqual(parsePlatformTrendsParams({}), DEFAULT_PLATFORM_TRENDS_VIEW);
  assert.deepEqual(DEFAULT_PLATFORM_TRENDS_VIEW, {
    span: "30d",
    from: "",
    to: "",
    compare: "previous",
  });
});

test("an address with a span and a comparison names that reading", () => {
  assert.deepEqual(parsePlatformTrendsParams({ span: "12m", compare: "year_earlier" }), {
    span: "12m",
    from: "",
    to: "",
    compare: "year_earlier",
  });
});

test("an unknown span or comparison falls back to the default, each on its own", () => {
  assert.deepEqual(parsePlatformTrendsParams({ span: "fortnight", compare: "year_earlier" }), {
    ...DEFAULT_PLATFORM_TRENDS_VIEW,
    compare: "year_earlier",
  });
  assert.deepEqual(parsePlatformTrendsParams({ span: "7d", compare: "last_decade" }), {
    ...DEFAULT_PLATFORM_TRENDS_VIEW,
    span: "7d",
  });
});

test("a custom range carries its two days", () => {
  assert.deepEqual(parsePlatformTrendsParams({ span: "custom", from: "2026-03-01", to: "2026-04-15" }), {
    span: "custom",
    from: "2026-03-01",
    to: "2026-04-15",
    compare: "previous",
  });
});

test("a custom range without two real calendar days falls back to the default span", () => {
  for (const params of [
    { span: "custom" },
    { span: "custom", from: "2026-03-01" },
    { span: "custom", from: "2026-03-01", to: "soon" },
    { span: "custom", from: "2026-02-30", to: "2026-03-05" },
    { span: "custom", from: "1/3/2026", to: "2026-03-05" },
  ]) {
    assert.deepEqual(parsePlatformTrendsParams(params), DEFAULT_PLATFORM_TRENDS_VIEW, JSON.stringify(params));
  }
});

test("a preset ignores days left in the address", () => {
  assert.deepEqual(parsePlatformTrendsParams({ span: "90d", from: "2026-03-01", to: "2026-04-15" }), {
    ...DEFAULT_PLATFORM_TRENDS_VIEW,
    span: "90d",
  });
});

test("a parameter repeated in the address is read by its first value", () => {
  assert.equal(parsePlatformTrendsParams({ span: ["7d", "90d"] }).span, "7d");
});

test("the default reading writes nothing to the address", () => {
  assert.equal(platformTrendsQuery(DEFAULT_PLATFORM_TRENDS_VIEW), "");
});

test("the query states what is not the default, and the parser reads it back", () => {
  const views = [
    { span: "7d", from: "", to: "", compare: "previous" },
    { span: "30d", from: "", to: "", compare: "year_earlier" },
    { span: "custom", from: "2026-03-01", to: "2026-04-15", compare: "year_earlier" },
  ] as const;
  assert.equal(platformTrendsQuery(views[0]), "?span=7d");
  assert.equal(platformTrendsQuery(views[1]), "?compare=year_earlier");
  assert.equal(platformTrendsQuery(views[2]), "?span=custom&from=2026-03-01&to=2026-04-15&compare=year_earlier");
  for (const view of views) {
    const params = Object.fromEntries(new URLSearchParams(platformTrendsQuery(view)));
    assert.deepEqual(parsePlatformTrendsParams(params), view);
  }
});

test("the request is the address's own query on the section's route", () => {
  assert.equal(platformTrendsRequestPath(DEFAULT_PLATFORM_TRENDS_VIEW), PLATFORM_TRENDS_API_PATH);
  assert.equal(
    platformTrendsRequestPath({ span: "custom", from: "2026-03-01", to: "2026-04-15", compare: "previous" }),
    `${PLATFORM_TRENDS_API_PATH}?span=custom&from=2026-03-01&to=2026-04-15`,
  );
});

test("choosing a preset drops the custom days and keeps the comparison", () => {
  const custom = { span: "custom", from: "2026-03-01", to: "2026-04-15", compare: "year_earlier" } as const;
  assert.deepEqual(platformTrendsViewAfterSpanChange(custom, "90d", null), {
    span: "90d",
    from: "",
    to: "",
    compare: "year_earlier",
  });
});

test("choosing custom starts from the span on screen, as the backend stated it", () => {
  assert.deepEqual(
    platformTrendsViewAfterSpanChange(DEFAULT_PLATFORM_TRENDS_VIEW, "custom", {
      first_day: "2026-06-07",
      last_day: "2026-07-06",
    }),
    { span: "custom", from: "2026-06-07", to: "2026-07-06", compare: "previous" },
  );
});

test("choosing custom with no reading on screen changes nothing", () => {
  assert.deepEqual(
    platformTrendsViewAfterSpanChange(DEFAULT_PLATFORM_TRENDS_VIEW, "custom", null),
    DEFAULT_PLATFORM_TRENDS_VIEW,
  );
});

test("a day of the custom range changes alone, and an emptied day is not taken", () => {
  const custom = { span: "custom", from: "2026-03-01", to: "2026-04-15", compare: "previous" } as const;
  assert.deepEqual(platformTrendsViewAfterRangeChange(custom, { from: "2026-02-01" }), {
    ...custom,
    from: "2026-02-01",
  });
  assert.deepEqual(platformTrendsViewAfterRangeChange(custom, { to: "" }), custom);
});

test("the comparison changes alone", () => {
  assert.deepEqual(platformTrendsViewAfterComparisonChange(DEFAULT_PLATFORM_TRENDS_VIEW, "year_earlier"), {
    ...DEFAULT_PLATFORM_TRENDS_VIEW,
    compare: "year_earlier",
  });
});

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

test("a change is a signed percentage in the reader's marks", () => {
  assert.equal(formatTrendChange(0.125, "en"), "+12.5%");
  assert.equal(formatTrendChange(-0.1, "en"), "-10%");
  assert.equal(formatTrendChange(0, "en"), "0%");
  assert.equal(formatTrendChange(2, "en"), "+200%");
  // A change too small to print carries no sign: the tile calls it no change.
  assert.equal(formatTrendChange(0.0004, "en"), "0%");
  assert.equal(formatTrendChange(-0.0004, "en"), "0%");
  // Spanish puts a comma in the fraction; the space before the sign is the
  // locale's own and may be a no-break one.
  assert.match(formatTrendChange(0.125, "es"), /^\+12,5\s?%$/);
  assert.match(formatTrendChange(-0.1, "es"), /^-10\s?%$/);
});

test("the smallest change the tile draws an arrow for prints as more than 0%", () => {
  // The tile rounds a change to a thousandth of the ratio and reads its
  // direction from that (roundedTrendChange in the UI package). Printed any
  // coarser than one decimal of a percent, the smallest step would be an
  // arrow beside "0%" again.
  assert.equal(formatTrendChange(0.001, "en"), "+0.1%");
  assert.equal(formatTrendChange(-0.001, "en"), "-0.1%");
  assert.match(formatTrendChange(0.001, "es"), /^\+0,1\s?%$/);
  assert.match(formatTrendChange(-0.001, "es"), /^-0,1\s?%$/);
});

test("a mean keeps one decimal at most", () => {
  assert.equal(formatTrendMean(3.5, "en"), "3.5");
  assert.equal(formatTrendMean(3, "en"), "3");
  assert.equal(formatTrendMean(1234.567, "en"), "1,234.6");
  assert.equal(formatTrendMean(3.5, "es"), "3,5");
});

test("a daily bucket is named by its day and a weekly one by its first and last", () => {
  const day: PlatformTrendBucket = bucket("2026-07-06", 1);
  assert.equal(formatTrendBucket(day, "en"), "Jul 6");
  assert.equal(formatTrendBucket(day, "es"), "6 jul");
  const week: PlatformTrendBucket = { first_day: "2026-06-29", last_day: "2026-07-05", value: 1, partial: false };
  const label = formatTrendBucket(week, "en");
  assert.ok(label.startsWith("Jun 29"), label);
  assert.ok(label.endsWith("Jul 5"), label);
  assert.ok(!label.includes("\u2014"), "no em dash in a range");
});

// ---------------------------------------------------------------------------
// Response to chart input
// ---------------------------------------------------------------------------

function bucket(day: string, value: number, partial = false): PlatformTrendBucket {
  return { first_day: day, last_day: day, value, partial };
}

function series(key: string, values: number[], extra: Partial<PlatformTrendSeries> = {}): PlatformTrendSeries {
  return {
    key,
    total: values.reduce((sum, value) => sum + value, 0),
    buckets: values.map((value, index) => bucket(`2026-07-0${index + 1}`, value)),
    today: null,
    comparison_total: null,
    change: null,
    comparison_buckets: values.map(() => null),
    ...extra,
  };
}

function figure(all: PlatformTrendSeries[], extra: Partial<PlatformTrendFigure> = {}): PlatformTrendFigure {
  return {
    recorded: true,
    exact_since: null,
    data_since: "2026-01-01",
    total: all.reduce((sum, entry) => sum + entry.total, 0),
    comparison_total: null,
    change: null,
    series: all,
    ...extra,
  };
}

const label = (entry: PlatformTrendBucket) => entry.first_day;

test("a series becomes one bucket per bucket, with its comparison beside it", () => {
  const data = seriesChartData(
    [
      series("all", [4, 0, 7], {
        comparison_buckets: [null, bucket("2026-06-02", 0), bucket("2026-06-03", 9)],
      }),
    ],
    label,
  );
  assert.deepEqual(
    data.map((datum) => [datum.key, datum.label, datum.values.all, datum.comparison?.[comparisonSeriesId("all")]]),
    [
      ["2026-07-01", "2026-07-01", 4, null],
      ["2026-07-02", "2026-07-02", 0, 0],
      ["2026-07-03", "2026-07-03", 7, 9],
    ],
  );
  assert.ok(data.every((datum) => datum.partial !== true));
});

test("today is the last bucket, marked partial, with no comparison", () => {
  const data = seriesChartData([series("all", [4, 5], { today: bucket("2026-07-03", 2, true) })], label);
  const today = data.at(-1)!;
  assert.equal(data.length, 3);
  assert.equal(today.key, "2026-07-03");
  assert.equal(today.partial, true);
  assert.equal(today.values.all, 2);
  assert.equal(today.comparison?.[comparisonSeriesId("all")] ?? null, null);
});

test("a custom range that ends before yesterday has no today", () => {
  const data = seriesChartData([series("all", [4, 5])], label);
  assert.equal(data.length, 2);
});

test("a stack carries each Sales Channel, its height, and the comparison's height", () => {
  const data = stackedChartData(
    [
      series("online", [3, 1], {
        comparison_buckets: [null, bucket("2026-06-02", 2)],
        today: bucket("2026-07-03", 1, true),
      }),
      series("in_person", [2, 0], {
        comparison_buckets: [null, bucket("2026-06-02", 5)],
        today: bucket("2026-07-03", 4, true),
      }),
    ],
    label,
  );
  assert.deepEqual(data[0]!.values, { online: 3, in_person: 2 });
  assert.equal(data[0]!.total, 5);
  // Before the data began the comparison is a gap, not a zero.
  assert.equal(data[0]!.comparison?.[STACK_COMPARISON_ID], null);
  assert.equal(data[1]!.comparison?.[STACK_COMPARISON_ID], 7);
  assert.equal(data[2]!.partial, true);
  assert.equal(data[2]!.total, 5);
});

test("an activity figure is drawn on the span's buckets, a bucket with no figure as a gap", () => {
  const span = [bucket("2026-07-01", 0), bucket("2026-07-02", 0), bucket("2026-07-03", 0)];
  const data = activityChartData(
    activity({
      buckets: [null, { first_day: "2026-07-02", last_day: "2026-07-02", value: 0 }, { first_day: "2026-07-03", last_day: "2026-07-03", value: 2.5 }],
      comparison_buckets: [{ first_day: "2026-06-01", last_day: "2026-06-01", value: 1 }, null, null],
    }),
    span,
    label,
  );
  assert.deepEqual(
    data.map((datum) => [datum.key, datum.values.all, datum.comparison?.[comparisonSeriesId("all")]]),
    [
      ["2026-07-01", null, 1],
      ["2026-07-02", 0, null],
      ["2026-07-03", 2.5, null],
    ],
  );
  // The daily close freezes no today, so the chart has none.
  assert.ok(data.every((datum) => datum.partial !== true));
});

test("the span's buckets are read from a figure that always states them", () => {
  const trends = { figures: { page_views: figure([series("all", [1, 2])]) } } as unknown as PlatformTrends;
  assert.deepEqual(
    spanBuckets(trends).map((entry) => entry.first_day),
    ["2026-07-01", "2026-07-02"],
  );
});

test("the marker stands on the bucket that holds the day recording began", () => {
  const weeks = [
    { key: "2026-06-22", firstDay: "2026-06-22", lastDay: "2026-06-28" },
    { key: "2026-06-29", firstDay: "2026-06-29", lastDay: "2026-07-05" },
  ];
  assert.equal(markerBucketKey(weeks, "2026-07-01"), "2026-06-29");
  assert.equal(markerBucketKey(weeks, "2026-06-22"), "2026-06-22");
  // Before the span, after it, or never: no marker.
  assert.equal(markerBucketKey(weeks, "2026-01-01"), null);
  assert.equal(markerBucketKey(weeks, "2026-07-06"), null);
  assert.equal(markerBucketKey(weeks, null), null);
});

test("Active Organizations is sales only throughout while no bucket shown is past its marker", () => {
  const shown = [
    { key: "2026-07-01", firstDay: "2026-07-01", lastDay: "2026-07-01" },
    { key: "2026-07-02", firstDay: "2026-07-02", lastDay: "2026-07-02" },
  ];
  assert.equal(salesOnlyThroughout(activity({ sale_half_only: true, exact_since: null }), shown), true);
  assert.equal(salesOnlyThroughout(activity({ exact_since: "2026-07-03" }), shown), true);
  assert.equal(salesOnlyThroughout(activity({ exact_since: "2026-07-02" }), shown), false);
  assert.equal(salesOnlyThroughout(activity({ exact_since: "2026-01-01" }), shown), false);
  assert.equal(salesOnlyThroughout(activity({ exact_since: "2026-07-03" }), []), false);
});

test("a chart wholly before the day exact recording began has no marker to say so", () => {
  const shown = [{ key: "2026-07-01", firstDay: "2026-07-01", lastDay: "2026-07-02" }];
  assert.equal(allBeforeMarker("2026-07-03", shown), true);
  assert.equal(allBeforeMarker("2026-07-02", shown), false);
  // Always exact, or nothing drawn.
  assert.equal(allBeforeMarker(null, shown), false);
  assert.equal(allBeforeMarker("2026-07-03", []), false);
});

test("a change stated against a comparison span that begins before a Member's work was counted reaches sales-only days", () => {
  const comparison = { first_day: "2026-06-01", last_day: "2026-06-30" };
  assert.equal(comparisonReachesSalesOnly(activity({ exact_since: "2026-06-15" }), comparison), true);
  assert.equal(comparisonReachesSalesOnly(activity({ exact_since: null, sale_half_only: true }), comparison), true);
  assert.equal(comparisonReachesSalesOnly(activity({ exact_since: "2026-06-01" }), comparison), false);
  assert.equal(comparisonReachesSalesOnly(activity({ exact_since: "2026-01-01" }), comparison), false);
});

test("with no change stated there is nothing for the sales-only comparison note to qualify", () => {
  const comparison = { first_day: "2026-06-01", last_day: "2026-06-30" };
  // The tile says there is nothing to compare against; the note speaks of
  // "the change" and is not shown.
  assert.equal(
    comparisonReachesSalesOnly(activity({ exact_since: "2026-06-15", change: null }), comparison),
    false,
  );
  assert.equal(
    comparisonReachesSalesOnly(activity({ exact_since: null, sale_half_only: true, change: null }), comparison),
    false,
  );
  // A change that prints as 0% is still a change stated.
  assert.equal(comparisonReachesSalesOnly(activity({ exact_since: "2026-06-15", change: 0 }), comparison), true);
});

test("the axis makes room for the comparison and for the stack", () => {
  const lines = seriesChartData(
    [series("all", [4, 3], { comparison_buckets: [bucket("2026-06-01", 18), null] })],
    label,
  );
  assert.equal(trendChartYMax(lines), 18);
  const stack = stackedChartData([series("online", [30, 1]), series("import", [40, 1])], label);
  assert.equal(trendChartYMax(stack), 70);
  // Tickets Sold reads the height of the whole stack: 1,014 across its Sales
  // Channels tops the axis at 1,200, not the 2,000 the old ladder gave (#836).
  const busy = stackedChartData([series("online", [900]), series("import", [114])], label);
  assert.equal(trendChartYMax(busy), 1_200);
  assert.equal(trendChartYMax([]), 1);
});

// ---------------------------------------------------------------------------
// Response to tile input
// ---------------------------------------------------------------------------

const count = (value: number) => `#${value}`;

/** The comparison span most tiles here are read against: wholly after every
 * fixture's data_since (2026-01-01). */
const COMPARED = { first_day: "2026-06-01", last_day: "2026-06-30" };

test("a count tile states the figure's total, its change and the shape of the span", () => {
  const tile = countTile(
    figure([series("all", [4, 0, 7], { today: bucket("2026-07-04", 99, true) })], { change: 0.25 }),
    COMPARED,
    count,
  );
  // Today is in no total and not in the trend line either.
  assert.deepEqual(tile.readings, [{ id: "all", value: "#11", change: 0.25, trend: [4, 0, 7] }]);
  assert.equal(tile.secondary, undefined);
});

test("an absent change stays absent: null, never a zero", () => {
  const tile = countTile(figure([series("all", [4])], { change: null }), COMPARED, count);
  assert.equal(tile.readings[0]!.change, null);
});

// Why a tile states no change (#837). The change itself is the backend's and
// stays absent; the tile says which of three reasons holds.

const comparedBucket = (day: string, value: number) => bucket(day, value);

test("a count tile with no comparison data says there is nothing to compare against", () => {
  // The comparison span ends before the data begins: no comparison bucket.
  const tile = countTile(
    figure([series("all", [4, 1])], { data_since: "2026-07-01", comparison_total: null, change: null }),
    COMPARED,
    count,
  );
  assert.equal(tile.readings[0]!.change, null);
  assert.equal(tile.readings[0]!.noChange, "nothingToCompare");
});

test("a count tile compared with a span the data covers in part says the comparison is only partly recorded", () => {
  // Events Published: data begins mid-comparison, so the buckets from that day
  // are drawn and the change is absent (#826), whatever their total.
  for (const comparisonTotal of [3, 0]) {
    const tile = countTile(
      figure(
        [
          series("all", [4, 1], {
            comparison_total: comparisonTotal,
            comparison_buckets: [null, comparedBucket("2026-06-20", comparisonTotal)],
          }),
        ],
        { data_since: "2026-06-15", comparison_total: comparisonTotal, change: null },
      ),
      COMPARED,
      count,
    );
    assert.equal(tile.readings[0]!.change, null);
    assert.equal(tile.readings[0]!.noChange, "partlyRecorded", `comparison total ${comparisonTotal}`);
  }
});

test("a count tile compared with a fully recorded span that came to zero says there was none in it", () => {
  // Page Views: the comparison is recorded throughout and is a real zero, and
  // a change from zero has no ratio.
  const tile = countTile(
    figure(
      [
        series("all", [4, 1], {
          comparison_total: 0,
          comparison_buckets: [comparedBucket("2026-06-01", 0), comparedBucket("2026-06-02", 0)],
        }),
      ],
      { comparison_total: 0, change: null },
    ),
    COMPARED,
    count,
  );
  assert.equal(tile.readings[0]!.change, null);
  assert.equal(tile.readings[0]!.noChange, "noneCompared");
});

test("a comparison that begins on the day the data begins is fully recorded", () => {
  const tile = countTile(
    figure([series("all", [4])], { data_since: COMPARED.first_day, comparison_total: 0, change: null }),
    COMPARED,
    count,
  );
  assert.equal(tile.readings[0]!.noChange, "noneCompared");
});

test("a figure with no data at all has nothing to compare against", () => {
  const tile = countTile(
    figure([series("all", [0])], { data_since: null, comparison_total: null, change: null }),
    COMPARED,
    count,
  );
  assert.equal(tile.readings[0]!.noChange, "nothingToCompare");
});

test("a stated change has no reason for its absence, and neither has a figure that is not recorded", () => {
  const stated = countTile(figure([series("all", [4])], { comparison_total: 2, change: 1 }), COMPARED, count);
  assert.equal(stated.readings[0]!.change, 1);
  assert.equal("noChange" in stated.readings[0]!, false);
  const notRecorded = countTile(figure([series("all", [4])], { recorded: false }), COMPARED, count);
  assert.equal("noChange" in notRecorded.readings[0]!, false);
  assert.equal("change" in notRecorded.readings[0]!, false);
});

test("a money reading says why its own currency states no change", () => {
  const tile = moneyTile(
    figure(
      [
        series("USD", [100], { comparison_total: 0, change: null }),
        series("EUR", [100], { comparison_total: null, change: null }),
        series("COP", [100], { comparison_total: 50, change: 1 }),
      ],
      { total: null },
    ),
    COMPARED,
    (cents) => String(cents),
  );
  assert.deepEqual(
    tile.readings.map((reading) => [reading.id, reading.change, reading.noChange]),
    [
      ["USD", null, "noneCompared"],
      ["EUR", null, "nothingToCompare"],
      ["COP", 1, undefined],
    ],
  );
});

test("an activity tile with no frozen day in the comparison says there is nothing to compare against", () => {
  const tile = activityTile(
    activity({ data_since: "2026-07-01", comparison_mean: null, change: null, comparison_buckets: [null] }),
    COMPARED,
    { mean: String, count },
  );
  assert.equal(tile.readings[0]!.change, null);
  assert.equal(tile.readings[0]!.noChange, "nothingToCompare");
});

test("an activity tile compared with a span its data covers in part says the comparison is only partly recorded", () => {
  for (const comparisonMean of [2, 0]) {
    const tile = activityTile(
      activity({
        data_since: "2026-06-15",
        comparison_mean: comparisonMean,
        change: null,
        comparison_buckets: [null, { first_day: "2026-06-15", last_day: "2026-06-21", value: comparisonMean }],
      }),
      COMPARED,
      { mean: String, count },
    );
    assert.equal(tile.readings[0]!.change, null);
    assert.equal(tile.readings[0]!.noChange, "partlyRecorded", `comparison mean ${comparisonMean}`);
  }
});

test("an activity tile compared with a fully recorded span whose mean is zero says there was none in it", () => {
  const tile = activityTile(
    activity({
      comparison_mean: 0,
      change: null,
      comparison_buckets: [{ first_day: "2026-06-01", last_day: "2026-06-07", value: 0 }],
    }),
    COMPARED,
    { mean: String, count },
  );
  assert.equal(tile.readings[0]!.change, null);
  assert.equal(tile.readings[0]!.noChange, "noneCompared");
});

test("an activity tile with a stated change has no reason for its absence", () => {
  const tile = activityTile(activity(), COMPARED, { mean: String, count });
  assert.equal(tile.readings[0]!.change, 0.25);
  assert.equal("noChange" in tile.readings[0]!, false);
});

test("Tickets Sold draws the height of its stack and states each Sales Channel beneath", () => {
  const tile = countTile(figure([series("online", [3, 1]), series("in_person", [2, 0])], { change: -0.5 }), COMPARED, count, {
    splitBySeries: true,
  });
  assert.deepEqual(tile.readings, [{ id: "all", value: "#6", change: -0.5, trend: [5, 1] }]);
  assert.deepEqual(tile.secondary, [
    { id: "online", value: "#4" },
    { id: "in_person", value: "#2" },
  ]);
});

test("a figure that is not recorded says so, with no change and no line", () => {
  const tile = countTile(figure([series("all", [0, 0])], { recorded: false }), COMPARED, count);
  assert.deepEqual(tile.readings, [{ id: "all", value: null, absence: "notRecorded" }]);
  assert.equal(tile.secondary, undefined);
});

test("a money tile states each currency on its own, never a sum", () => {
  const tile = moneyTile(
    figure([series("USD", [1000, 250], { change: 0.1 }), series("EUR", [0, 900], { change: null })], { total: null }),
    COMPARED,
    (cents, currency) => `${currency} ${cents / 100}`,
  );
  assert.deepEqual(tile.readings, [
    { id: "USD", currency: "USD", value: "USD 12.5", change: 0.1, trend: [1000, 250] },
    { id: "EUR", currency: "EUR", value: "EUR 9", change: null, noChange: "nothingToCompare", trend: [0, 900] },
  ]);
});

test("a money tile with no currency yet has no figure", () => {
  const tile = moneyTile(figure([], { total: null }), COMPARED, () => "never");
  assert.deepEqual(tile.readings, [{ id: "none", value: null, absence: "noFigure" }]);
});

test("an activity tile states the mean a day, its change, and the 7-day and 30-day counts", () => {
  const tile = activityTile(
    activity({
      buckets: [null, { first_day: "2026-07-02", last_day: "2026-07-02", value: 3 }],
    }),
    COMPARED,
    { mean: (value) => `~${value}`, count },
  );
  assert.deepEqual(tile.readings, [
    { id: "all", value: "~3.5", dailyMean: true, change: 0.25, trend: [null, 3] },
  ]);
  assert.deepEqual(tile.secondary, [
    { id: "distinct_7d", value: "#12" },
    { id: "distinct_30d", value: "#40" },
  ]);
});

test("Active Customers is not recorded while recording is off: no figure, no change, never a zero", () => {
  const tile = activityTile(
    activity({
      recorded: false,
      exact_since: null,
      data_since: null,
      mean: null,
      comparison_mean: null,
      change: null,
      distinct_7d: null,
      distinct_30d: null,
      buckets: [null, null],
      comparison_buckets: [null, null],
    }),
    COMPARED,
    { mean: String, count },
  );
  assert.deepEqual(tile.readings, [{ id: "all", value: null, absence: "notRecorded" }]);
  assert.equal(tile.secondary, undefined);
});

test("a recorded activity figure with no frozen day in the span has no figure and no counts", () => {
  const tile = activityTile(
    activity({ mean: null, change: null, distinct_7d: null, distinct_30d: null, buckets: [null] }),
    COMPARED,
    { mean: String, count },
  );
  assert.deepEqual(tile.readings, [
    { id: "all", value: null, absence: "noFigure", change: null, noChange: "nothingToCompare", trend: [null] },
  ]);
  assert.equal(tile.secondary, undefined);
});

function activity(extra: Partial<PlatformTrendActivityFigure> = {}): PlatformTrendActivityFigure {
  return {
    recorded: true,
    exact_since: "2026-01-01",
    data_since: "2026-01-01",
    sale_half_only: false,
    mean: 3.5,
    comparison_mean: 2.8,
    change: 0.25,
    distinct_7d: 12,
    distinct_30d: 40,
    buckets: [],
    comparison_buckets: [],
    ...extra,
  };
}

test("platformTrendsScrollAnchor changes with the span, the comparison and a custom range, and with nothing else", () => {
  const reading = (span: Partial<PlatformTrends["span"]>, comparison: Partial<PlatformTrends["comparison"]> = {}) =>
    ({
      timezone: "America/Montevideo",
      span: { preset: "30d", first_day: "2026-10-21", last_day: "2026-11-19", ...span },
      comparison: { kind: "previous", first_day: "2026-09-21", last_day: "2026-10-20", ...comparison },
      granularity: "day",
      today: "2026-11-19",
      figures: {},
    }) as unknown as PlatformTrends;
  const anchor = platformTrendsScrollAnchor(reading({}));

  // The same reading fetched again is the same data, even with today's figures
  // moved on: a hand-scrolled chart stays put.
  assert.equal(platformTrendsScrollAnchor(reading({})), anchor);
  const refetched = { ...reading({}), figures: { page_views: {} } } as unknown as PlatformTrends;
  assert.equal(platformTrendsScrollAnchor(refetched), anchor);

  // Another span, comparison or custom range is new data, opened at its newest end again.
  assert.notEqual(platformTrendsScrollAnchor(reading({ preset: "90d", first_day: "2026-08-22" })), anchor);
  assert.notEqual(
    platformTrendsScrollAnchor(reading({}, { kind: "year_earlier", first_day: "2025-10-21", last_day: "2025-11-19" })),
    anchor,
  );
  const custom = reading({ preset: "custom", first_day: "2026-10-01", last_day: "2026-10-31" });
  assert.notEqual(
    platformTrendsScrollAnchor(reading({ preset: "custom", first_day: "2026-10-01", last_day: "2026-11-05" })),
    platformTrendsScrollAnchor(custom),
  );
});
