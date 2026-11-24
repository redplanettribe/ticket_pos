import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_ORGANIZATION_TRENDS_VIEW,
  ORGANIZATION_TRENDS_API_PATH,
  cumulativeStackData,
  dailyStackData,
  formatOrganizationTrendBucket,
  organizationTrendTile,
  organizationTrendsQuery,
  organizationTrendsRequestPath,
  organizationTrendsViewAfterComparisonChange,
  organizationTrendsViewAfterRangeChange,
  organizationTrendsViewAfterSpanChange,
  parseOrganizationTrendsParams,
  countedNothing,
  type OrganizationTrendDays,
  type OrganizationTrendFigure,
  type OrganizationTrendSeries,
} from "./organization-trends.ts";
import { STACK_COMPARISON_ID } from "./trends-chart.ts";

// ---------------------------------------------------------------------------
// The address
// ---------------------------------------------------------------------------

test("a bare address is 30 days against the span before", () => {
  assert.deepEqual(parseOrganizationTrendsParams({}), DEFAULT_ORGANIZATION_TRENDS_VIEW);
  assert.equal(organizationTrendsQuery(DEFAULT_ORGANIZATION_TRENDS_VIEW), "");
});

test("a span and a comparison are read off the address and written back the same", () => {
  const view = parseOrganizationTrendsParams({ span: "12m", compare: "year" });
  assert.deepEqual(view, { span: "12m", from: "", to: "", compare: "year" });
  assert.equal(organizationTrendsQuery(view), "?span=12m&compare=year");
  assert.deepEqual(parseOrganizationTrendsParams(Object.fromEntries(new URLSearchParams("span=12m&compare=year"))), view);
});

test("all time carries no comparison, in the view or the address", () => {
  const view = parseOrganizationTrendsParams({ span: "all", compare: "year" });
  assert.deepEqual(view, { span: "all", from: "", to: "", compare: "previous" });
  assert.equal(organizationTrendsQuery({ ...view, compare: "year" }), "?span=all");
});

test("a custom range keeps its two days", () => {
  const view = parseOrganizationTrendsParams({ span: "custom", from: "2026-01-15", to: "2026-06-10" });
  assert.deepEqual(view, { span: "custom", from: "2026-01-15", to: "2026-06-10", compare: "previous" });
  assert.equal(organizationTrendsQuery(view), "?span=custom&from=2026-01-15&to=2026-06-10");
  assert.equal(
    organizationTrendsRequestPath(view),
    `${ORGANIZATION_TRENDS_API_PATH}?span=custom&from=2026-01-15&to=2026-06-10`,
  );
});

test("an unknown span or comparison, or a custom range without two real days, falls back on its own", () => {
  assert.deepEqual(parseOrganizationTrendsParams({ span: "52w", compare: "year" }), {
    ...DEFAULT_ORGANIZATION_TRENDS_VIEW,
    compare: "year",
  });
  assert.deepEqual(parseOrganizationTrendsParams({ span: "7d", compare: "year_earlier" }), {
    span: "7d",
    from: "",
    to: "",
    compare: "previous",
  });
  assert.deepEqual(parseOrganizationTrendsParams({ span: "custom", from: "2026-02-30", to: "2026-03-01" }), DEFAULT_ORGANIZATION_TRENDS_VIEW);
  assert.deepEqual(parseOrganizationTrendsParams({ span: ["90d", "7d"] }).span, "90d");
});

test("choosing a span: a preset drops the days, all time the comparison, custom starts from the span shown", () => {
  const view = { span: "custom" as const, from: "2026-01-01", to: "2026-01-31", compare: "year" as const };
  assert.deepEqual(organizationTrendsViewAfterSpanChange(view, "7d", null), { span: "7d", from: "", to: "", compare: "year" });
  assert.deepEqual(organizationTrendsViewAfterSpanChange(view, "all", null), {
    span: "all",
    from: "",
    to: "",
    compare: "previous",
  });
  const shown = { first_day: "2026-06-07", last_day: "2026-07-06" };
  assert.deepEqual(organizationTrendsViewAfterSpanChange(DEFAULT_ORGANIZATION_TRENDS_VIEW, "custom", shown), {
    span: "custom",
    from: "2026-06-07",
    to: "2026-07-06",
    compare: "previous",
  });
  assert.equal(organizationTrendsViewAfterSpanChange(DEFAULT_ORGANIZATION_TRENDS_VIEW, "custom", null), DEFAULT_ORGANIZATION_TRENDS_VIEW);
  assert.deepEqual(organizationTrendsViewAfterRangeChange(view, { from: "", to: "2026-02-10" }), { ...view, to: "2026-02-10" });
  assert.deepEqual(organizationTrendsViewAfterComparisonChange(view, "previous"), { ...view, compare: "previous" });
});

// ---------------------------------------------------------------------------
// Buckets
// ---------------------------------------------------------------------------

test("a whole month is named by its month, a cut one by its days, a day by its day", () => {
  assert.equal(
    formatOrganizationTrendBucket({ first_day: "2026-06-01", last_day: "2026-06-30", partial: false }, "month", "en"),
    "Jun 2026",
  );
  assert.match(
    formatOrganizationTrendBucket({ first_day: "2026-07-01", last_day: "2026-07-06", partial: true }, "month", "en"),
    /^Jul 1\s*[-–]\s*6, 2026$/,
  );
  assert.equal(formatOrganizationTrendBucket({ first_day: "2026-07-07", last_day: "2026-07-07", partial: true }, "month", "en"), "Jul 7");
  assert.equal(formatOrganizationTrendBucket({ first_day: "2026-07-03", last_day: "2026-07-03", partial: false }, "day", "es"), "3 jul");
});

function bucket(first: string, last: string, value: number, partial = false) {
  return { first_day: first, last_day: last, value, partial };
}

const span: OrganizationTrendDays[] = [
  { first_day: "2026-05-15", last_day: "2026-05-31", partial: true },
  { first_day: "2026-06-01", last_day: "2026-06-30", partial: false },
  { first_day: "2026-07-01", last_day: "2026-07-06", partial: true },
];

function series(key: string, values: number[], comparison: number[], today: number | null): OrganizationTrendSeries {
  return {
    key,
    total: values.reduce((sum, value) => sum + value, 0),
    buckets: span.map((days, index) => bucket(days.first_day, days.last_day, values[index] ?? 0, days.partial)),
    today: today === null ? null : bucket("2026-07-07", "2026-07-07", today, true),
    comparison_total: comparison.length === 0 ? null : comparison.reduce((sum, value) => sum + value, 0),
    change: null,
    comparison_buckets: comparison.map((value, index) => bucket(`c${index}`, `c${index}`, value)),
  };
}

const figure: OrganizationTrendFigure = {
  total: 21,
  comparison_total: 6,
  change: 2.5,
  series: [series("online", [1, 2, 3], [1, 1, 1], 4), series("in_person", [0, 0, 0], [0, 0, 0], 0), series("import", [5, 0, 10], [3, 0, 0], 0)],
};

const label = (days: OrganizationTrendDays) => `${days.first_day}${days.partial ? "*" : ""}`;

test("the Daily view stacks each bucket, flags cut months and today partial, and sets the comparison stack beside it", () => {
  const data = dailyStackData(figure, span, label);
  assert.equal(data.length, 4);
  assert.deepEqual(
    data.map((datum) => [datum.label, datum.total, datum.partial === true]),
    [
      ["2026-05-15*", 6, true],
      ["2026-06-01", 2, false],
      ["2026-07-01*", 13, true],
      ["2026-07-07", 4, true],
    ],
  );
  assert.deepEqual(data[0].values, { online: 1, in_person: 0, import: 5 });
  assert.deepEqual(data[0].comparison, { [STACK_COMPARISON_ID]: 4 });
  // Today is set beside nothing.
  assert.equal(data[3].comparison, undefined);
});

test("the Cumulative view runs from zero at the span's start to its total, and leaves today out", () => {
  const data = cumulativeStackData(figure, span, label);
  assert.deepEqual(
    data.map((datum) => datum.total),
    [6, 8, 21],
  );
  assert.deepEqual(data[2].values, { online: 6, in_person: 0, import: 15 });
  assert.equal(data.at(-1)?.total, figure.total);
});

test("a span with no sales is drawn as zeros, never as gaps", () => {
  const nothing: OrganizationTrendFigure = {
    total: 0,
    comparison_total: 0,
    change: null,
    series: [series("online", [0, 0, 0], [0, 0, 0], 0)],
  };
  const daily = dailyStackData(nothing, span, label);
  assert.ok(daily.every((datum) => datum.total === 0));
  assert.ok(cumulativeStackData(nothing, span, label).every((datum) => datum.total === 0));
  assert.equal(countedNothing(nothing), true);
  assert.equal(countedNothing(figure), false);
  // Today alone selling is not nothing.
  assert.equal(countedNothing({ ...nothing, series: [series("online", [0, 0, 0], [], 2)] }), false);
});

// ---------------------------------------------------------------------------
// Tiles
// ---------------------------------------------------------------------------

test("a tile states the total, the change and the stack's height bucket by bucket, today left out", () => {
  const tile = organizationTrendTile(figure, true, String);
  assert.deepEqual(tile, { id: "all", value: "21", change: 2.5, trend: [6, 2, 13] });
});

test("a comparison that came to zero says so; all time states no change at all", () => {
  const zero = organizationTrendTile({ ...figure, comparison_total: 0, change: null }, true, String);
  assert.equal(zero.change, null);
  assert.equal(zero.noChange, "noneCompared");
  const allTime = organizationTrendTile({ ...figure, comparison_total: null, change: null }, false, String);
  assert.equal("change" in allTime, false);
  assert.equal(allTime.noChange, undefined);
});
