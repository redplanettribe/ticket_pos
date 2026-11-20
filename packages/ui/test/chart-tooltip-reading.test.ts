import assert from "node:assert/strict";
import test from "node:test";

import {
  barTooltipRows,
  comparisonTooltipRows,
  tooltipEmptyLabel,
  tooltipNotes,
  tooltipRows,
} from "../src/components/charts/chart-tooltip-rows.ts";

const series = [
  { id: "a", name: "Online", color: "#111" },
  { id: "b", name: "In person", color: "#222" },
];
const comparison = [{ id: "previous", name: "Previous 30 days", color: "#111" }];

const formatValue = (value: number) => `v${value}`;
const noDetail = () => null;

// --- not recorded extends "nothing counted" ----------------------------------

test("a bucket in which every series is a gap was not recorded", () => {
  const reading = tooltipRows(series, { a: null }, formatValue, noDetail, { zeroIsMeasured: true });
  assert.equal(reading.nothingCounted, true);
  assert.equal(reading.notRecorded, true);
});

test("a bucket of zeros on a counting view counted nothing, but was recorded", () => {
  const reading = tooltipRows(series, { a: 0, b: 0 }, formatValue, noDetail, { zeroIsMeasured: false });
  assert.equal(reading.nothingCounted, true);
  assert.equal(reading.notRecorded, false);
});

test("a bucket with a row is neither", () => {
  const reading = tooltipRows(series, { a: 0, b: null }, formatValue, noDetail, { zeroIsMeasured: true });
  assert.deepEqual(
    reading.rows.map((row) => row.value),
    ["v0"],
  );
  assert.equal(reading.nothingCounted, false);
  assert.equal(reading.notRecorded, false);
});

test("an unrecorded bucket says so when the chart has the words for it", () => {
  const labels = { nothingCountedLabel: "Nothing counted", notRecordedLabel: "Not recorded" };
  assert.equal(tooltipEmptyLabel({ nothingCounted: true, notRecorded: true }, labels), "Not recorded");
  assert.equal(tooltipEmptyLabel({ nothingCounted: true, notRecorded: false }, labels), "Nothing counted");
});

test("a chart without the words reads an unrecorded bucket as it always did", () => {
  const labels = { nothingCountedLabel: "Nothing counted" };
  assert.equal(tooltipEmptyLabel({ nothingCounted: true, notRecorded: true }, labels), "Nothing counted");
});

test("a bucket with rows has no empty line, and a chart with no words has none either", () => {
  const labels = { nothingCountedLabel: "Nothing counted", notRecordedLabel: "Not recorded" };
  assert.equal(tooltipEmptyLabel({ nothingCounted: false, notRecorded: false }, labels), undefined);
  assert.equal(tooltipEmptyLabel({ nothingCounted: true, notRecorded: true }, {}), undefined);
});

// --- the comparison series in the hover card ---------------------------------

test("a comparison row names its series, states its value and is marked faint", () => {
  assert.deepEqual(comparisonTooltipRows(comparison, { comparison: { previous: 12 } }, formatValue), [
    { id: "comparison:previous", name: "Previous 30 days", color: "#111", value: "v12", faint: true },
  ]);
});

test("a comparison's zero is stated: its span was counted and came to nothing", () => {
  const rows = comparisonTooltipRows(comparison, { comparison: { previous: 0 } }, formatValue);
  assert.deepEqual(
    rows.map((row) => row.value),
    ["v0"],
  );
});

test("a bucket the comparison does not reach has no comparison row", () => {
  assert.deepEqual(comparisonTooltipRows(comparison, {}, formatValue), []);
  assert.deepEqual(comparisonTooltipRows(comparison, { comparison: { previous: null } }, formatValue), []);
});

test("a chart with no comparison series has no comparison rows", () => {
  assert.deepEqual(comparisonTooltipRows(undefined, { comparison: { previous: 3 } }, formatValue), []);
});

test("a comparison row's id cannot collide with a series of the same id", () => {
  const rows = comparisonTooltipRows([{ id: "a", name: "Before", color: "#111" }], { comparison: { a: 1 } }, formatValue);
  assert.notEqual(rows[0]?.id, "a");
});

// --- the partial bucket in the hover card ------------------------------------

test("the hover card of a partial bucket says it is partial", () => {
  assert.deepEqual(tooltipNotes({ partial: true }, { partialLabel: "Today so far" }), ["Today so far"]);
});

test("a closed bucket carries no note", () => {
  assert.deepEqual(tooltipNotes({ partial: false }, { partialLabel: "Today so far" }), []);
  assert.deepEqual(tooltipNotes({}, { partialLabel: "Today so far" }), []);
});

test("a chart with no words for it says nothing of a partial bucket", () => {
  assert.deepEqual(tooltipNotes({ partial: true }, {}), []);
});

// --- a bar chart's hover card -------------------------------------------------

test("a bar chart states every drawn series, a zero and a value left out included", () => {
  const { rows, nothingCounted, notRecorded } = barTooltipRows(series, { a: 0 }, formatValue, (id) => `detail ${id}`);
  assert.deepEqual(rows, [
    { id: "a", name: "Online", color: "#111", value: "v0", detail: "detail a" },
    { id: "b", name: "In person", color: "#222", value: "v0", detail: "detail b" },
  ]);
  assert.equal(nothingCounted, false);
  assert.equal(notRecorded, false);
});

test("a series not recorded in the bucket has no row on a bar chart, and the others keep theirs", () => {
  const { rows, notRecorded } = barTooltipRows(series, { a: null, b: 4 }, formatValue, noDetail);
  assert.deepEqual(
    rows.map((row) => [row.id, row.value]),
    [["b", "v4"]],
  );
  assert.equal(notRecorded, false);
});

test("a bar chart bucket in which no series was recorded says so", () => {
  const reading = barTooltipRows(series, { a: null, b: null }, formatValue, noDetail);
  assert.deepEqual(reading.rows, []);
  assert.equal(tooltipEmptyLabel(reading, { notRecordedLabel: "Not recorded" }), "Not recorded");
  // Without the words the card is as empty as it would have been.
  assert.equal(tooltipEmptyLabel(reading, {}), undefined);
});

test("a bar chart drawing no series has an empty card and nothing to say of it", () => {
  const reading = barTooltipRows([], {}, formatValue, noDetail);
  assert.equal(reading.nothingCounted, false);
  assert.equal(reading.notRecorded, false);
});
