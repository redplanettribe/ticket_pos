import assert from "node:assert/strict";
import test from "node:test";

import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { TrendTile, type TrendTileReading } from "../src/components/charts/trend-tile.tsx";

const changeCopy = {
  format: (ratio: number) => `${ratio > 0 ? "+" : ""}${ratio * 100}%`,
  growth: "Up",
  decline: "Down",
  unchanged: "No change",
  nothingToCompare: "Nothing to compare against",
};

function tile(reading: Omit<TrendTileReading, "id" | "value">): string {
  return renderToStaticMarkup(
    React.createElement(TrendTile, {
      label: "Page Views",
      readings: [{ id: "all", value: "12", ...reading }],
      changeCopy,
    }),
  );
}

test("a null change says why when the reading says so (#837)", () => {
  const markup = tile({ change: null, noChange: "None in the span compared" });
  assert.match(markup, /None in the span compared/);
  assert.doesNotMatch(markup, /Nothing to compare against/);
});

test("a null change with no reason given says there is nothing to compare against", () => {
  assert.match(tile({ change: null }), /Nothing to compare against/);
});

test("a stated change is stated, whatever reason a reading carries", () => {
  const markup = tile({ change: 0.5, noChange: "None in the span compared" });
  assert.match(markup, /\+50%/);
  assert.doesNotMatch(markup, /None in the span compared/);
});

test("a reading that states nothing about a change says nothing about one", () => {
  const markup = tile({ noChange: "None in the span compared" });
  assert.doesNotMatch(markup, /None in the span compared|Nothing to compare against/);
});
