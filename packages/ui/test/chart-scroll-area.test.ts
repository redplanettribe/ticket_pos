import { container, setLayout, type FakeElement } from "./support/fake-dom.ts";

import assert from "node:assert/strict";
import test from "node:test";

import * as React from "react";
import { createRoot } from "react-dom/client";

import { CHART_SCROLL_AREA_ATTRIBUTE } from "../src/components/charts/chart-geometry.ts";
import { ChartScrollArea } from "../src/components/charts/chart-scroll-area.tsx";

// A 375px phone: the card's scroll area is 375px wide and a chart drawn wider
// than that overflows it by the difference.
const CARD_WIDTH = 375;
const CHART_WIDTH = 900;
const NEWEST_END = CHART_WIDTH - CARD_WIDTH;

setLayout((element) => {
  if (!(CHART_SCROLL_AREA_ATTRIBUTE in element.attributes)) {
    return { clientWidth: 0, scrollWidth: 0 };
  }
  const drawn = Number.parseFloat(element.firstChild?.style.width ?? "0") || 0;
  return { clientWidth: CARD_WIDTH, scrollWidth: Math.max(CARD_WIDTH, drawn) };
});

type Window = { key: string; chartWidth: number; hovered?: string } | { key?: undefined; chartWidth: number };

/** Mounts a scroll area holding a chart drawn at `chartWidth`, and re-renders it on demand. */
async function mount(first: Window) {
  const root = createRoot(container() as unknown as HTMLElement);
  let area: FakeElement | null = null;
  const render = (window: Window) =>
    React.act(async () =>
      root.render(
        React.createElement(
          ChartScrollArea,
          {
            ariaLabel: "Page Views, scrolls sideways",
            ref: (element: HTMLDivElement | null) => {
              area = element as unknown as FakeElement | null;
            },
            openAtNewestEnd:
              window.key === undefined ? undefined : { key: window.key, contentWidth: window.chartWidth },
          },
          React.createElement("div", { style: { width: window.chartWidth } }, "hovered" in window ? window.hovered : null),
        ),
      ),
    );
  await render(first);
  return {
    render,
    get area(): FakeElement {
      assert.ok(area, "the caller's ref is handed the scrolling element");
      return area;
    },
  };
}

test("a chart wider than its card opens scrolled to its newest end", async () => {
  const chart = await mount({ key: "30d|previous", chartWidth: CHART_WIDTH });

  assert.equal(chart.area.scrollLeft, NEWEST_END);
});

test("a scroll area not asked to open at its newest end opens at the start, as before", async () => {
  const chart = await mount({ chartWidth: CHART_WIDTH });

  assert.equal(chart.area.scrollLeft, 0);
});

test("a chart measured after mount opens at its newest end once it is wide enough to scroll", async () => {
  // The plot is measured over the frames after mount: the first render draws it
  // at no width, so there is nothing to scroll and nothing to anchor yet.
  const chart = await mount({ key: "30d|previous", chartWidth: 0 });
  assert.equal(chart.area.scrollLeft, 0);

  await chart.render({ key: "30d|previous", chartWidth: CHART_WIDTH });

  assert.equal(chart.area.scrollLeft, NEWEST_END);
});

test("scrolling by hand is not undone by a hover or a re-render with the same data", async () => {
  const chart = await mount({ key: "30d|previous", chartWidth: CHART_WIDTH });
  chart.area.scrollLeft = 120;

  await chart.render({ key: "30d|previous", chartWidth: CHART_WIDTH, hovered: "18 Nov" });
  await chart.render({ key: "30d|previous", chartWidth: CHART_WIDTH });

  assert.equal(chart.area.scrollLeft, 120);
});

test("a new span or comparison scrolls back to the newest end", async () => {
  const chart = await mount({ key: "30d|previous", chartWidth: CHART_WIDTH });
  chart.area.scrollLeft = 120;

  await chart.render({ key: "30d|year", chartWidth: CHART_WIDTH });
  assert.equal(chart.area.scrollLeft, NEWEST_END);

  chart.area.scrollLeft = 40;
  await chart.render({ key: "90d|year", chartWidth: CHART_WIDTH + 300 });
  assert.equal(chart.area.scrollLeft, NEWEST_END + 300);
});

test("a span that fits its card re-anchors when the next one no longer does", async () => {
  // A span that fits never takes its anchor (there is no end to open on), and
  // the one after it must not find the anchor already taken.
  const chart = await mount({ key: "7d|previous", chartWidth: CARD_WIDTH - 50 });
  assert.equal(chart.area.scrollLeft, 0);

  await chart.render({ key: "90d|previous", chartWidth: CHART_WIDTH });

  assert.equal(chart.area.scrollLeft, NEWEST_END);
});
