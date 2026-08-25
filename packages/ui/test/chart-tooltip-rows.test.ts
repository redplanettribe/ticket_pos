import assert from "node:assert/strict";
import test from "node:test";

import { tooltipRows } from "../src/components/charts/chart-tooltip-rows.ts";

// Three drawn series in the order the chart draws them: the whole page first,
// then two links.
const series = [
  { id: "page", name: "All page views", color: "#111" },
  { id: "a", name: "Poster", color: "#222" },
  { id: "b", name: "Newsletter", color: "#333" },
];

const formatValue = (value: number) => `v${value}`;
const noDetail = () => null;

// --- a counting view: a zero is nothing counted ------------------------------

test("a series that counted nothing in the bucket is not a row", () => {
  const { rows, nothingCounted } = tooltipRows(
    series,
    { page: 12, a: 0, b: 3 },
    formatValue,
    noDetail,
    { zeroIsMeasured: false },
  );
  assert.deepEqual(
    rows.map((row) => row.id),
    ["page", "b"],
  );
  assert.equal(nothingCounted, false);
});

test("a row states the series as drawn and the value as the caller spells it", () => {
  const { rows } = tooltipRows(series, { page: 0, a: 5, b: 0 }, formatValue, (id) => `detail ${id}`, {
    zeroIsMeasured: false,
  });
  assert.deepEqual(rows, [{ id: "a", name: "Poster", color: "#222", value: "v5", detail: "detail a" }]);
});

test("a zero with a detail is still not a row on a counting view", () => {
  // The Attributed-sales view states "0 tickets" under every link in every
  // bucket; that detail is bookkeeping, not a measurement.
  const { rows } = tooltipRows(series, { page: 1, a: 0, b: 0 }, formatValue, () => "0 tickets", {
    zeroIsMeasured: false,
  });
  assert.deepEqual(
    rows.map((row) => row.id),
    ["page"],
  );
});

// --- a measured view: a gap is nothing, a zero is a finding -------------------

test("a gap (null) is not a row", () => {
  const { rows, nothingCounted } = tooltipRows(
    series,
    { page: 0.5, a: null, b: 0.25 },
    formatValue,
    noDetail,
    { zeroIsMeasured: true },
  );
  assert.deepEqual(
    rows.map((row) => row.id),
    ["page", "b"],
  );
  assert.equal(nothingCounted, false);
});

test("a 0% rate is a row, with its detail", () => {
  const { rows } = tooltipRows(
    series,
    { page: null, a: 0, b: null },
    formatValue,
    (id) => (id === "a" ? "0 sales / 4 clicks" : null),
    { zeroIsMeasured: true },
  );
  assert.deepEqual(rows, [{ id: "a", name: "Poster", color: "#222", value: "v0", detail: "0 sales / 4 clicks" }]);
});

test("a series missing from the bucket altogether is read as a gap", () => {
  const { rows } = tooltipRows(series, { a: 0 }, formatValue, noDetail, { zeroIsMeasured: true });
  assert.deepEqual(
    rows.map((row) => row.id),
    ["a"],
  );
});

// --- order and the empty card -------------------------------------------------

test("rows keep the order the chart draws the series in, whatever the values", () => {
  const { rows } = tooltipRows(series, { b: 1, a: 100, page: 2 }, formatValue, noDetail, {
    zeroIsMeasured: false,
  });
  assert.deepEqual(
    rows.map((row) => row.id),
    ["page", "a", "b"],
  );
});

test("nothing counted is flagged only when every row drops", () => {
  const counting = tooltipRows(series, { page: 0, a: 0, b: 0 }, formatValue, noDetail, {
    zeroIsMeasured: false,
  });
  assert.deepEqual(counting.rows, []);
  assert.equal(counting.nothingCounted, true);

  const measured = tooltipRows(series, { page: null, a: null, b: null }, formatValue, noDetail, {
    zeroIsMeasured: true,
  });
  assert.deepEqual(measured.rows, []);
  assert.equal(measured.nothingCounted, true);
});

test("one surviving row is enough to keep the card", () => {
  const { rows, nothingCounted } = tooltipRows(series, { page: 0, a: 0, b: 1 }, formatValue, noDetail, {
    zeroIsMeasured: false,
  });
  assert.equal(rows.length, 1);
  assert.equal(nothingCounted, false);
});

test("a chart with no series drawn has nothing counted", () => {
  const { rows, nothingCounted } = tooltipRows([], { page: 3 }, formatValue, noDetail, {
    zeroIsMeasured: false,
  });
  assert.deepEqual(rows, []);
  assert.equal(nothingCounted, true);
});
