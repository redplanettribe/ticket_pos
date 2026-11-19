import assert from "node:assert/strict";
import test from "node:test";

import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { CHART_MARGIN, Y_AXIS_WIDTH } from "../src/components/charts/chart-geometry.ts";
import { MultiSeriesBarChart } from "../src/components/charts/multi-series-bar-chart.tsx";
import { MultiSeriesLineChart } from "../src/components/charts/multi-series-line-chart.tsx";

// The trend surfaces' minimum plot width (`TRENDS_MIN_PLOT_WIDTH` in the staff
// app): a card narrower than the chart drawn at it scrolls sideways.
const PLOT_WIDTH = 360;
const CHART_WIDTH = Y_AXIS_WIDTH + PLOT_WIDTH + CHART_MARGIN.right;
const NOT_RECORDED = "Active Customers is not recorded on this platform yet, so there is nothing to draw.";

const series = [{ id: "figure", name: "Active Customers", color: "#123456" }];
const days = (value: number | null) =>
  ["17 ago", "18 ago", "19 ago"].map((label) => ({ key: label, label, values: { figure: value } }));

function lineChart(value: number | null): string {
  return renderToStaticMarkup(
    React.createElement(MultiSeriesLineChart, {
      data: days(value),
      series,
      yMax: 10,
      plotWidth: PLOT_WIDTH,
      formatValue: String,
      nothingCountedLabel: "Nothing counted",
      notRecordedLabel: NOT_RECORDED,
      ariaLabel: "Active Customers by day",
    }),
  );
}

function barChart(value: number | null): string {
  return renderToStaticMarkup(
    React.createElement(MultiSeriesBarChart, {
      data: days(value),
      series,
      yMax: 10,
      plotWidth: PLOT_WIDTH,
      formatValue: String,
      notRecordedLabel: NOT_RECORDED,
      ariaLabel: "Page views by day",
    }),
  );
}

/** The opening tag of the element carrying `attribute`, so a test reads what
 * that element alone was told. */
function openingTag(markup: string, attribute: string): string {
  const tag = markup.match(new RegExp(`<[a-z]+[^>]*${attribute}[^>]*>`))?.[0];
  assert.ok(tag, `no element with ${attribute} in ${markup}`);
  return tag;
}

// --- the not-recorded box fits its card, not the chart's minimum width (#833) -

for (const [shape, render] of [
  ["line", lineChart],
  ["bar", barChart],
] as const) {
  test(`a not-recorded ${shape} chart is not drawn at the chart's minimum width`, () => {
    const markup = render(null);
    assert.ok(markup.includes(NOT_RECORDED), "the message is said");
    // No days to scroll through, so nothing in the box may be as wide as the
    // plot it stands in for: in a card narrower than that it would overflow
    // the scroll area, and its right border and its words would be cut off.
    assert.doesNotMatch(markup, new RegExp(`width:\\s*${CHART_WIDTH}px`));
    assert.doesNotMatch(markup, /\bw-max\b/);
    assert.doesNotMatch(markup, /\bsticky\b/);
  });

  test(`a not-recorded ${shape} chart takes the visible width of its card`, () => {
    const box = openingTag(render(null), 'role="group"');
    assert.match(box, /\bw-full\b/);
  });

  test(`a ${shape} chart with data keeps its minimum plot width, so it still scrolls`, () => {
    const plot = openingTag(render(4), 'role="img"');
    assert.match(plot, new RegExp(`width:\\s*${CHART_WIDTH}px`));
  });
}
