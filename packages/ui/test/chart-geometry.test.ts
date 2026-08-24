import assert from "node:assert/strict";
import test from "node:test";

import {
  CHART_MARGIN,
  CHART_PLOT_INSET,
  TOOLTIP_CHROME_HEIGHT,
  TOOLTIP_CURSOR_GAP,
  TOOLTIP_DETAIL_ROW_HEIGHT,
  TOOLTIP_ROW_HEIGHT,
  Y_AXIS_WIDTH,
  scrollWindowShift,
  tooltipColumnLayout,
  xAxisEdgePadding,
  xAxisProps,
  xTickInterval,
  xTickLabels,
} from "../src/components/charts/chart-geometry.ts";

// --- the X axis inset that keeps the end labels whole ------------------------

const dailyLabels = ["17 ago", "18 ago", "19 ago", "20 ago", "21 ago", "22 ago", "23 ago", "24 ago"];
const hourlyLabels = ["24 ago, 08:00", "24 ago, 09:00", "24 ago, 10:00"];

test("the inset is half the widest label, so a centred label ends inside the plot", () => {
  // "24 ago" is six characters; at the estimated width that is 39px across,
  // so the axis gives up 20px at each end for it.
  assert.equal(xAxisEdgePadding(dailyLabels), 20);
});

test("an hourly label earns a wider inset than a daily one", () => {
  assert.ok(xAxisEdgePadding(hourlyLabels) > xAxisEdgePadding(dailyLabels));
  assert.equal(xAxisEdgePadding(hourlyLabels), 43);
});

test("the inset is capped, so an outlandish label cannot eat the plot", () => {
  assert.equal(xAxisEdgePadding(["a label long enough to be a sentence about the bucket"]), 48);
});

test("the inset has a floor, so a point on the edge keeps its whole stroke", () => {
  assert.equal(xAxisEdgePadding(["1"]), 8);
  assert.equal(xAxisEdgePadding([]), 8);
});

test("the axis props inset both ends by the same amount and label by the key", () => {
  const props = xAxisProps(dailyLabels, 800);
  assert.deepEqual(props.padding, { left: 20, right: 20 });
  assert.equal(props.dataKey, "label");
  assert.equal(props.axisLine, false);
  assert.equal(props.tickLine, false);
});

// --- thinning the X ticks ----------------------------------------------------

test("wide buckets label every bucket", () => {
  assert.equal(xTickInterval(7, 700), 0);
});

test("narrow buckets skip enough to keep labels about 72px apart", () => {
  // 168 hourly buckets across 1680px is 10px a bucket: every eighth is labelled.
  assert.equal(xTickInterval(168, 1680), 7);
});

test("the inset comes out of the room the buckets share", () => {
  // 20 buckets in 400px is 20px each — three skipped. Inset by 40 a side they
  // have 320px, 16px each, and a fourth must be skipped.
  assert.equal(xTickInterval(20, 400), 3);
  assert.equal(xTickInterval(20, 400, 40), 4);
});

test("nothing to label, or no room, is interval zero rather than a division by zero", () => {
  assert.equal(xTickInterval(0, 800), 0);
  assert.equal(xTickInterval(10, 0), 0);
  assert.equal(xTickInterval(10, 40, 20), 0);
});

// --- which buckets are labelled ----------------------------------------------

/** A week of hours as the Affiliate Trends axis labels it, oldest first. */
const weekOfHours = Array.from({ length: 168 }, (_, index) => `h${index}`);

test("the last bucket is always labelled, however the count divides", () => {
  // 168 buckets at 24px, inset 43 a side: every fourth from the first would
  // end at h164 and leave the current hour — the one the reader is scrolled
  // to — unnamed. h164 stands only three buckets from the end, so it gives way.
  const ticks = xTickLabels(weekOfHours, 168 * 24, 43);
  assert.equal(ticks[0], "h0");
  assert.equal(ticks[ticks.length - 1], "h167");
  assert.ok(!ticks.includes("h164"));
  assert.equal(ticks[ticks.length - 2], "h160");
});

test("the regular labels keep their even spacing from the first", () => {
  const ticks = xTickLabels(weekOfHours, 168 * 24, 43);
  const step = xTickInterval(168, 168 * 24, 43) + 1;
  for (const [index, tick] of ticks.slice(0, -1).entries()) {
    assert.equal(tick, `h${index * step}`);
  }
});

test("a last-but-one label far enough from the end is kept", () => {
  // Fifteen buckets at about 10.5px are labelled every seventh: h7 stands a
  // whole interval from h14, so both are drawn.
  assert.equal(xTickInterval(15, 158), 6);
  assert.deepEqual(xTickLabels(weekOfHours.slice(0, 15), 158), ["h0", "h7", "h14"]);
});

test("wide buckets label every bucket, the last included, exactly once", () => {
  const labels = ["17 ago", "18 ago", "19 ago", "20 ago", "21 ago", "22 ago", "23 ago", "24 ago"];
  assert.deepEqual(xTickLabels(labels, 800, 20), labels);
});

test("a lone bucket is labelled once, and no buckets not at all", () => {
  assert.deepEqual(xTickLabels(["24 ago"], 400), ["24 ago"]);
  assert.deepEqual(xTickLabels([], 400), []);
});

test("the axis props state the labels as ticks and leave recharts nothing to thin", () => {
  const props = xAxisProps(weekOfHours, 168 * 24);
  assert.equal(props.interval, 0);
  assert.equal(props.ticks[0], "h0");
  assert.equal(props.ticks[props.ticks.length - 1], "h167");
});

// --- the hover card's columns ------------------------------------------------

/** The room a 320px chart leaves a card with no footer. */
const roomIn320 = 320 - 2 * CHART_MARGIN.top - TOOLTIP_CHROME_HEIGHT;

test("a handful of rows is one column", () => {
  assert.deepEqual(tooltipColumnLayout(3, TOOLTIP_ROW_HEIGHT, roomIn320), {
    columns: 1,
    rowsPerColumn: 3,
  });
});

test("seventeen plain rows do not fit a 320px chart and are dealt into two balanced columns", () => {
  // Sixteen Affiliate Links and All page views: taller than the chart in one
  // column, so nine and eight rather than fourteen and three.
  assert.deepEqual(tooltipColumnLayout(17, TOOLTIP_ROW_HEIGHT, roomIn320), {
    columns: 2,
    rowsPerColumn: 9,
  });
});

test("rows with a detail line are taller, so the same seventeen need three columns", () => {
  const layout = tooltipColumnLayout(17, TOOLTIP_DETAIL_ROW_HEIGHT, roomIn320);
  assert.deepEqual(layout, { columns: 3, rowsPerColumn: 6 });
  // And the tallest column still fits the room it was laid out for.
  assert.ok(layout.rowsPerColumn * TOOLTIP_DETAIL_ROW_HEIGHT <= roomIn320);
});

test("every column of the layout fits the room", () => {
  for (const rows of [1, 5, 14, 15, 17, 30, 64]) {
    for (const rowHeight of [TOOLTIP_ROW_HEIGHT, TOOLTIP_DETAIL_ROW_HEIGHT]) {
      const layout = tooltipColumnLayout(rows, rowHeight, roomIn320);
      assert.ok(layout.rowsPerColumn * rowHeight <= roomIn320, `${rows} rows of ${rowHeight}px`);
      assert.ok(layout.columns * layout.rowsPerColumn >= rows, `${rows} rows all placed`);
    }
  }
});

test("a chart too short for even one row still lays out one row per column", () => {
  assert.deepEqual(tooltipColumnLayout(4, TOOLTIP_ROW_HEIGHT, 10), { columns: 4, rowsPerColumn: 1 });
});

test("no rows is a single empty column, not a division by zero", () => {
  assert.deepEqual(tooltipColumnLayout(0, TOOLTIP_ROW_HEIGHT, roomIn320), {
    columns: 1,
    rowsPerColumn: 1,
  });
});

// --- keeping the hover card in the scroll window -----------------------------

const seen = { start: 100, end: 946 };

test("a card inside the window is left where recharts put it", () => {
  assert.equal(scrollWindowShift({ start: 400, size: 200 }, seen), 0);
  assert.equal(scrollWindowShift({ start: 746, size: 200 }, seen), 0);
});

test("a card past the window's end is moved back by exactly the overhang", () => {
  // Placed at 868 with 207px to say, as it was over the last hourly buckets.
  assert.equal(scrollWindowShift({ start: 868, size: 207 }, seen), -(868 + 207 - 946));
});

test("given the cursor gap, a card past the end mirrors itself to the cursor's other side", () => {
  // Recharts put it 10px after a cursor at 858; the mirror ends 10px before
  // the cursor, so it starts at 858 - 10 - 207 = 641.
  assert.equal(scrollWindowShift({ start: 868, size: 207 }, seen, TOOLTIP_CURSOR_GAP), 641 - 868);
});

test("a card that would not fit on the cursor's other side is moved back instead", () => {
  // A 500px card 10px after a cursor at 500 would start at -10 mirrored, before
  // the window, so it is pulled back to the window's end.
  assert.equal(scrollWindowShift({ start: 510, size: 500 }, seen, TOOLTIP_CURSOR_GAP), -(510 + 500 - 946));
});

test("a card before the window's start is moved on to it", () => {
  assert.equal(scrollWindowShift({ start: 60, size: 200 }, seen), 40);
});

test("a card wider than the window is pinned to its start", () => {
  assert.equal(scrollWindowShift({ start: 500, size: 900 }, seen), -400);
});

// --- the geometry the Staff app imports keeps its meaning --------------------

test("the plot inset is still the Y axis and the right margin", () => {
  assert.equal(CHART_PLOT_INSET, Y_AXIS_WIDTH + CHART_MARGIN.right);
  assert.equal(CHART_MARGIN.left, 0);
});
