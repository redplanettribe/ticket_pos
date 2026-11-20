import assert from "node:assert/strict";
import test from "node:test";

import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { compile } from "tailwindcss";

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

/** A tile with a caption and two lesser figures, whose figures are `figure`. */
function tileWith(figure: string): string {
  return renderToStaticMarkup(
    React.createElement(TrendTile, {
      label: "Active Organizations",
      readings: [{ id: "all", value: figure, caption: "a day", change: 0.5, trend: [1, 2, 3] }],
      secondary: [
        { id: "distinct_7d", label: "Different over 7 days", value: figure },
        { id: "distinct_30d", label: "Different over 30 days", value: figure },
      ],
      changeCopy,
    }),
  );
}

/** Every class the markup names, once. */
function everyClassIn(markup: string): string[] {
  const classes = new Set<string>();
  for (const [, list] of markup.matchAll(/class="([^"]*)"/g)) {
    for (const name of list!.split(/\s+/)) if (name !== "") classes.add(name);
  }
  return [...classes];
}

/** The class list of the first element of `tag` in the markup. */
function classesOfFirst(markup: string, tag: string): string[] {
  const match = new RegExp(`<${tag}[^>]*class="([^"]*)"`).exec(markup);
  assert.ok(match, `no <${tag}> with a class in ${markup}`);
  return match[1]!.split(/\s+/);
}

async function compiledCss(classes: string[]): Promise<string> {
  const compiler = await compile("@tailwind utilities;", {});
  return compiler.build(classes);
}

test("nothing on a tile wraps onto another row because a figure is wider (#840)", async () => {
  const built = await compiledCss(everyClassIn(tileWith("1,234,567")));
  assert.doesNotMatch(built, /flex-wrap:\s*wrap/, built);
});

test("a tile's lesser figures sit one to a row (#840)", async () => {
  const built = await compiledCss(classesOfFirst(tileWith("3"), "dl"));
  assert.match(built, /display:\s*grid/, built);
  assert.doesNotMatch(built, /grid-template-columns|flex-direction:\s*row|display:\s*flex/, built);
});

test("a lesser figure's label and value stay on one line, the label giving way (#840)", async () => {
  const markup = tileWith("1,234,567");
  const label = await compiledCss(classesOfFirst(markup, "dt"));
  const value = await compiledCss(classesOfFirst(markup, "dd"));
  assert.match(label, /white-space:\s*nowrap/, label);
  assert.match(label, /text-overflow:\s*ellipsis/, label);
  assert.match(value, /white-space:\s*nowrap/, value);
  assert.match(value, /flex-shrink:\s*0/, value);
});
