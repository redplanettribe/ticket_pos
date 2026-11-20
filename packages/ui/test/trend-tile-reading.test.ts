import assert from "node:assert/strict";
import test from "node:test";

import { roundedTrendChange, sparklineShape, trendDirection } from "../src/components/charts/trend-tile-reading.ts";

// --- the four states of a change ----------------------------------------------

test("a positive change is growth and a negative one decline", () => {
  assert.equal(trendDirection(0.25), "growth");
  assert.equal(trendDirection(-0.4), "decline");
});

test("a change of exactly nothing is no change", () => {
  assert.equal(trendDirection(0), "unchanged");
  assert.equal(trendDirection(-0), "unchanged");
});

test("an absent change is nothing to compare against, which is not no change", () => {
  assert.equal(trendDirection(null), "nothing");
  assert.equal(trendDirection(undefined), "nothing");
});

test("a change that is not a number is nothing to compare against", () => {
  // A division by an empty comparison must never reach the tile, and if one
  // does it is not drawn as growth without end.
  assert.equal(trendDirection(Number.POSITIVE_INFINITY), "nothing");
  assert.equal(trendDirection(Number.NaN), "nothing");
});

test("a change that prints as 0% is no change, not growth or decline", () => {
  // The tile prints one decimal of a percent. A ratio too small to print is
  // not drawn with an arrow beside "0%".
  assert.equal(trendDirection(0.0004), "unchanged");
  assert.equal(trendDirection(-0.0004), "unchanged");
  assert.equal(roundedTrendChange(0.0004), 0);
  // Never a negative zero, which a formatter could sign.
  assert.ok(Object.is(roundedTrendChange(-0.0004), 0));
});

test("the smallest change that prints keeps its direction", () => {
  assert.equal(trendDirection(0.0005), "growth");
  assert.equal(trendDirection(-0.0005), "decline");
  assert.equal(trendDirection(0.001), "growth");
  assert.equal(trendDirection(-0.001), "decline");
  // Halves round away from zero on both sides, so growth and decline of the
  // same size print the same size.
  assert.equal(roundedTrendChange(0.0005), 0.001);
  assert.equal(roundedTrendChange(-0.0005), -0.001);
});

test("a change is rounded to the one decimal of a percent the tile prints", () => {
  assert.equal(roundedTrendChange(0.12549), 0.125);
  assert.equal(roundedTrendChange(-0.1), -0.1);
  assert.equal(roundedTrendChange(2), 2);
  assert.equal(roundedTrendChange(0), 0);
});

test("an absent change has no rounded change either", () => {
  assert.equal(roundedTrendChange(null), null);
  assert.equal(roundedTrendChange(undefined), null);
  assert.equal(roundedTrendChange(Number.NaN), null);
  assert.equal(roundedTrendChange(Number.NEGATIVE_INFINITY), null);
});

// --- the small trend line -----------------------------------------------------

const box = { width: 100, height: 20, inset: 2 };

test("the trend line spans the box, oldest bucket on the left, tallest at the top", () => {
  assert.deepEqual(sparklineShape([0, 5, 10], box), {
    runs: [
      [
        { x: 2, y: 18 },
        { x: 50, y: 10 },
        { x: 98, y: 2 },
      ],
    ],
    points: [],
  });
});

test("the trend line is scaled from zero, so a steady figure is not drawn as a floor", () => {
  const { runs } = sparklineShape([8, 8], box);
  assert.deepEqual(runs, [
    [
      { x: 2, y: 2 },
      { x: 98, y: 2 },
    ],
  ]);
});

test("a span of zeros is a flat line on the baseline", () => {
  const { runs } = sparklineShape([0, 0, 0], box);
  assert.deepEqual(
    runs[0]?.map((point) => point.y),
    [18, 18, 18],
  );
});

test("a bucket with no value breaks the line, and keeps its place on the axis", () => {
  const { runs, points } = sparklineShape([null, 4, 8, null, 8], box);
  assert.deepEqual(runs, [
    [
      { x: 26, y: 10 },
      { x: 50, y: 2 },
    ],
  ]);
  // The value with a gap on both sides has no segment to be part of; it is a
  // point of its own.
  assert.deepEqual(points, [{ x: 98, y: 2 }]);
});

test("a single bucket is a point in the middle of the box", () => {
  assert.deepEqual(sparklineShape([3], box), { runs: [], points: [{ x: 50, y: 2 }] });
});

test("a span with no value at all draws nothing", () => {
  assert.deepEqual(sparklineShape([null, null], box), { runs: [], points: [] });
  assert.deepEqual(sparklineShape([], box), { runs: [], points: [] });
});
