import assert from "node:assert/strict";
import test from "node:test";

import {
  firstParam,
  isCalendarDay,
  parseTrendSpanParams,
  trendSpanQuery,
  trendSpanViewAfterSpanChange,
  type TrendSpanScheme,
} from "./trends-span.ts";

// The address both trends surfaces share. Platform Trends' and Organization
// Trends' own tests read it through their schemes; these pin what the scheme
// itself decides: whether there is an all time span, which has no comparison.

type Span = "30d" | "all" | "custom";
type Comparison = "previous" | "year";

const withAllTime: TrendSpanScheme<Span, Comparison> = {
  spans: ["30d", "all", "custom"],
  comparisons: ["previous", "year"],
  defaultView: { span: "30d", from: "", to: "", compare: "previous" },
  allTime: "all",
};

const withoutAllTime: TrendSpanScheme<Span, Comparison> = { ...withAllTime, allTime: null };

test("a param repeated in the address is read by its first value, trimmed", () => {
  assert.equal(firstParam([" 7d ", "30d"]), "7d");
  assert.equal(firstParam(undefined), "");
});

test("a calendar day must exist on the calendar", () => {
  assert.equal(isCalendarDay("2026-02-28"), true);
  assert.equal(isCalendarDay("2026-02-30"), false);
  assert.equal(isCalendarDay("2026-2-28"), false);
});

test("all time drops the comparison on a scheme that has it, and nowhere else", () => {
  assert.deepEqual(parseTrendSpanParams(withAllTime, { span: "all", compare: "year" }), {
    span: "all",
    from: "",
    to: "",
    compare: "previous",
  });
  assert.deepEqual(parseTrendSpanParams(withoutAllTime, { span: "all", compare: "year" }), {
    span: "all",
    from: "",
    to: "",
    compare: "year",
  });
  assert.equal(trendSpanQuery(withAllTime, { span: "all", from: "", to: "", compare: "year" }), "?span=all");
  assert.equal(
    trendSpanQuery(withoutAllTime, { span: "all", from: "", to: "", compare: "year" }),
    "?span=all&compare=year",
  );
  const view = { span: "30d", from: "", to: "", compare: "year" } as const;
  assert.equal(trendSpanViewAfterSpanChange(withAllTime, view, "all", null).compare, "previous");
  assert.equal(trendSpanViewAfterSpanChange(withoutAllTime, view, "all", null).compare, "year");
});
