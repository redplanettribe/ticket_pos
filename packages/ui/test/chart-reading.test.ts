import assert from "node:assert/strict";
import test from "node:test";

import {
  bucketKeyTarget,
  bucketIndexAt,
  comparisonValue,
  initialBucketIndex,
  markerPlacement,
  nothingRecorded,
  partialLineValues,
} from "../src/components/charts/chart-reading.ts";

// --- the comparison series: aligned bucket to bucket, a gap where it is silent

test("a comparison value is read from the bucket it is aligned with", () => {
  assert.equal(comparisonValue({ comparison: { previous: 7 } }, "previous"), 7);
});

test("a comparison's zero is a zero, since the span it comes from was counted", () => {
  assert.equal(comparisonValue({ comparison: { previous: 0 } }, "previous"), 0);
});

test("a comparison with missing leading buckets is a gap there, never a zero", () => {
  // The comparison span reaches before the data begins: the first bucket has
  // no comparison at all, the second states a null, the third none for this
  // series. Each is null, which is the line chart's word for a gap.
  const buckets = [
    {},
    { comparison: { previous: null } },
    { comparison: { other: 4 } },
    { comparison: { previous: 3 } },
    { comparison: { previous: 5 } },
  ];
  assert.deepEqual(
    buckets.map((bucket) => comparisonValue(bucket, "previous")),
    [null, null, null, 3, 5],
  );
});

// --- the partial bucket: a line splits into what is closed and what is not ---

test("a line with no partial bucket is all closed and has no partial stretch", () => {
  assert.deepEqual(partialLineValues([1, 2, 3], [false, false, false]), {
    closed: [1, 2, 3],
    partial: [null, null, null],
  });
});

test("the partial bucket leaves the closed line and is reached by its own stretch", () => {
  // Today (the last bucket) is drawn by the partial stretch, which starts at
  // the last closed bucket so the two lines meet there.
  assert.deepEqual(partialLineValues([4, 6, 2], [false, false, true]), {
    closed: [4, 6, null],
    partial: [null, 6, 2],
  });
});

test("a partial bucket after a gap is a point on its own, with nothing leading to it", () => {
  assert.deepEqual(partialLineValues([4, null, 2], [false, false, true]), {
    closed: [4, null, null],
    partial: [null, null, 2],
  });
});

test("a partial bucket with no value draws nothing, and nothing leads to it", () => {
  assert.deepEqual(partialLineValues([4, 6, null], [false, false, true]), {
    closed: [4, 6, null],
    partial: [null, null, null],
  });
});

test("a partial bucket alone on the chart is still drawn", () => {
  assert.deepEqual(partialLineValues([9], [true]), { closed: [null], partial: [9] });
});

// --- the recording-began marker ----------------------------------------------

const days = ["d1", "d2", "d3", "d4", "d5", "d6"];

test("a marker stands at its bucket, labelled on the side with more room", () => {
  assert.deepEqual(markerPlacement(days, "d2"), { index: 1, labelSide: "after" });
  assert.deepEqual(markerPlacement(days, "d5"), { index: 4, labelSide: "before" });
});

test("a marker in the first bucket is labelled after it and in the last before it", () => {
  assert.deepEqual(markerPlacement(days, "d1"), { index: 0, labelSide: "after" });
  assert.deepEqual(markerPlacement(days, "d6"), { index: 5, labelSide: "before" });
});

test("a marker on the only bucket is labelled after it", () => {
  assert.deepEqual(markerPlacement(["d1"], "d1"), { index: 0, labelSide: "after" });
});

test("a marker for a bucket the chart does not draw is not drawn", () => {
  assert.equal(markerPlacement(days, "d0"), null);
  assert.equal(markerPlacement([], "d1"), null);
});

// --- bucket selection ---------------------------------------------------------

test("the keyboard starts on the selected bucket", () => {
  assert.equal(initialBucketIndex(days, "d3"), 2);
});

test("with nothing selected the keyboard starts on the newest bucket", () => {
  assert.equal(initialBucketIndex(days, null), 5);
  // A selection the chart no longer draws (the span changed) is no selection.
  assert.equal(initialBucketIndex(days, "gone"), 5);
});

test("a chart with no buckets has nowhere for the keyboard to start", () => {
  assert.equal(initialBucketIndex([], null), -1);
});

test("the arrow keys move one bucket and stop at the ends", () => {
  assert.equal(bucketKeyTarget("ArrowRight", 2, 6), 3);
  assert.equal(bucketKeyTarget("ArrowLeft", 2, 6), 1);
  assert.equal(bucketKeyTarget("ArrowRight", 5, 6), 5);
  assert.equal(bucketKeyTarget("ArrowLeft", 0, 6), 0);
});

test("Home and End go to the oldest and the newest bucket", () => {
  assert.equal(bucketKeyTarget("Home", 3, 6), 0);
  assert.equal(bucketKeyTarget("End", 3, 6), 5);
});

test("a key that moves nothing is left to the browser", () => {
  assert.equal(bucketKeyTarget("Tab", 3, 6), null);
  assert.equal(bucketKeyTarget("a", 3, 6), null);
  assert.equal(bucketKeyTarget("ArrowRight", 0, 0), null);
});

// Ten buckets on a plot 240 wide, inset 20 at each end: the buckets share 200.
const bars = { count: 10, plotWidth: 240, edgePadding: 20, shape: "band" } as const;
const line = { count: 11, plotWidth: 240, edgePadding: 20, shape: "point" } as const;

test("a click on bars chooses the band it landed in", () => {
  // Bands are 20 wide and start at the inset: [20, 40) is the first.
  assert.equal(bucketIndexAt(20, bars), 0);
  assert.equal(bucketIndexAt(39, bars), 0);
  assert.equal(bucketIndexAt(40, bars), 1);
  assert.equal(bucketIndexAt(219, bars), 9);
});

test("a click on a line chooses the nearest point", () => {
  // Points stand every 20 from the inset: 20, 40, 60 and on to 220.
  assert.equal(bucketIndexAt(20, line), 0);
  assert.equal(bucketIndexAt(29, line), 0);
  // Halfway goes to the earlier point, which is the one the hover card names.
  assert.equal(bucketIndexAt(30, line), 0);
  assert.equal(bucketIndexAt(31, line), 1);
  assert.equal(bucketIndexAt(220, line), 10);
});

test("a click in the inset at either end chooses the end bucket", () => {
  assert.equal(bucketIndexAt(0, bars), 0);
  assert.equal(bucketIndexAt(240, bars), 9);
  assert.equal(bucketIndexAt(5, line), 0);
  assert.equal(bucketIndexAt(235, line), 10);
});

test("a click outside the plot chooses nothing", () => {
  // Left of the plot is the Y axis, right of it the margin.
  assert.equal(bucketIndexAt(-1, bars), null);
  assert.equal(bucketIndexAt(241, bars), null);
  assert.equal(bucketIndexAt(Number.NaN, bars), null);
});

test("a chart with one bucket has one bucket to choose, wherever the click lands", () => {
  assert.equal(bucketIndexAt(3, { ...bars, count: 1 }), 0);
  assert.equal(bucketIndexAt(200, { ...line, count: 1 }), 0);
});

test("a chart with no buckets, or no room between its insets, has none to choose", () => {
  assert.equal(bucketIndexAt(100, { ...bars, count: 0 }), null);
  assert.equal(bucketIndexAt(20, { ...bars, plotWidth: 40 }), null);
});

// --- not recorded: distinct from a chart of zeros ----------------------------

test("a chart whose every value is null was not recorded", () => {
  const data = [{ values: { a: null, b: null } }, { values: {} }];
  assert.equal(nothingRecorded(data, ["a", "b"]), true);
});

test("a chart of zeros was recorded", () => {
  const data = [{ values: { a: 0 } }, { values: { a: 0 } }];
  assert.equal(nothingRecorded(data, ["a"]), false);
});

test("one recorded bucket makes the chart a recorded one", () => {
  const data = [{ values: { a: null } }, { values: { a: 3 } }];
  assert.equal(nothingRecorded(data, ["a"]), false);
});

test("only the drawn series decide whether the chart was recorded", () => {
  const data = [{ values: { a: null, hidden: 3 } }];
  assert.equal(nothingRecorded(data, ["a"]), true);
});

test("a chart with no buckets or no series is empty, not unrecorded", () => {
  assert.equal(nothingRecorded([], ["a"]), false);
  assert.equal(nothingRecorded([{ values: { a: null } }], []), false);
});
