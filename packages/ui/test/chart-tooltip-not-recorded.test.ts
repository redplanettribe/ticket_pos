import assert from "node:assert/strict";
import test from "node:test";

import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { StackedChartTooltip } from "../src/components/charts/chart-frame.tsx";
import { MultiSeriesLineTooltip } from "../src/components/charts/multi-series-line-chart.tsx";
import { StackedBarChart } from "../src/components/charts/stacked-bar-chart.tsx";

// A count figure's bucket before its data began is null (#842): nothing was
// counted there, and its hover card says so in the surface's words for an
// unrecorded bucket, never as a zero.
const NOT_RECORDED = "No figure was recorded for these days. This is not a zero.";
const NOTHING_COUNTED = "Nothing counted";

const formatValue = (value: number) => `#${value}`;
const comparison = (id: string) => [{ id: `comparison:${id}`, name: "Previous 30 days", color: "#111" }];

/** The text of the markup, tags dropped. */
function text(markup: string): string {
  return markup.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
}

function lineCard(value: number | null): string {
  const series = [{ id: "all", name: "Page Views", color: "#123456" }];
  return renderToStaticMarkup(
    React.createElement(MultiSeriesLineTooltip, {
      series,
      formatValue,
      zeroIsMeasured: true,
      nothingCountedLabel: NOTHING_COUNTED,
      chartHeight: 280,
      comparisonSeries: comparison("all"),
      notRecordedLabel: NOT_RECORDED,
      active: true,
      payload: [{ payload: { key: "2026-08-01", label: "Aug 1", values: { all: value }, comparison: { "comparison:all": null } } }],
    }),
  );
}

const channels = [
  { id: "online", name: "Online", color: "#111" },
  { id: "import", name: "Import", color: "#222" },
];

function stackCard(values: Record<string, number | null>, total: number | null): string {
  return renderToStaticMarkup(
    React.createElement(StackedChartTooltip, {
      series: channels,
      formatValue,
      totalLabel: "Total",
      chartHeight: 280,
      comparisonSeries: [{ id: "comparison:stack", name: "Previous 30 days", color: "#111" }],
      notRecordedLabel: NOT_RECORDED,
      active: true,
      payload: [{ payload: { key: "2026-08-01", label: "Aug 1", values, total, comparison: { "comparison:stack": null } } }],
    }),
  );
}

test("hovering a null bucket of a count line says it was not recorded, not 0", () => {
  const card = text(lineCard(null));
  assert.ok(card.includes(NOT_RECORDED), card);
  assert.ok(!card.includes("#0"), card);
  assert.ok(!card.includes(NOTHING_COUNTED), card);
});

test("a counted bucket of nothing on a count line is still a zero", () => {
  const card = text(lineCard(0));
  assert.ok(card.includes("#0"), card);
  assert.ok(!card.includes(NOT_RECORDED), card);
});

test("hovering a null bucket of a stack says it was not recorded: no part, no total, no 0", () => {
  const card = text(stackCard({ online: null, import: null }, null));
  assert.ok(card.includes(NOT_RECORDED), card);
  assert.ok(!card.includes("#0"), card);
  assert.ok(!card.includes("Online"), card);
  assert.ok(!card.includes("Total"), card);
});

test("a counted stack of nothing states every part and its total as zeros", () => {
  const card = text(stackCard({ online: 0, import: 0 }, 0));
  assert.ok(card.includes("Online #0"), card);
  assert.ok(card.includes("Total #0"), card);
  assert.ok(!card.includes(NOT_RECORDED), card);
});

test("a stack with nothing recorded in any bucket says so in place of the chart", () => {
  const data = ["Aug 1", "Aug 2"].map((label) => ({ key: label, label, values: { online: null, import: null }, total: null }));
  const markup = renderToStaticMarkup(
    React.createElement(StackedBarChart, {
      data,
      series: channels,
      yMax: 10,
      plotWidth: 360,
      formatValue,
      totalLabel: "Total",
      notRecordedLabel: NOT_RECORDED,
      ariaLabel: "Tickets Sold by day",
    }),
  );
  assert.ok(text(markup).includes(NOT_RECORDED), markup);
});
