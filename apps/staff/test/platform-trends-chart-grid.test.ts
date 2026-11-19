import assert from "node:assert/strict";
import test from "node:test";

import { compile } from "tailwindcss";

import { OVERVIEW_CHART_COLUMNS } from "../app/operator/overview-layout.ts";

// Tailwind's lg breakpoint, the width the charts go two to a row.
const LG = "@media (width >= 64rem)";

/** The CSS Tailwind writes for the chart grid's column classes. */
async function chartGridCss(): Promise<string> {
  const compiler = await compile(`@theme { --breakpoint-sm: 40rem; --breakpoint-md: 48rem; --breakpoint-lg: 64rem; } @tailwind utilities;`, {});
  return compiler.build(OVERVIEW_CHART_COLUMNS.split(/\s+/));
}

/** Each rule that sets grid-column, with the at-rules and selectors it sits in. */
function gridColumnRules(css: string): string[][] {
  const rules: string[][] = [];
  const open: string[] = [];
  for (const piece of css.split(/(?<=[{};])/)) {
    const text = piece.trim();
    if (text.endsWith("{")) open.push(text.slice(0, -1).trim());
    else if (text.endsWith("}")) open.pop();
    else if (text.startsWith("grid-column:")) rules.push([...open]);
  }
  return rules;
}

test("at two columns the last chart spans the row when the chart count is odd", async () => {
  const css = await chartGridCss();
  assert.match(css, /grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/);
  const rules = gridColumnRules(css);
  assert.equal(rules.length, 1, css);
  const [rule] = rules;
  assert.ok(rule.includes(LG), `only from lg up: ${rule.join(" ")}`);
  // A direct child of the grid that is both its last and an odd one: the
  // ninth of nine, the seventh of seven, never the eighth of eight.
  assert.ok(rule.some((selector) => selector.includes("& > *")), rule.join(" "));
  assert.ok(rule.includes("&:last-child"), rule.join(" "));
  assert.ok(rule.includes("&:nth-child(odd)"), rule.join(" "));
  assert.match(css, /grid-column: span 2 \/ span 2/);
});

test("at one column no chart is told to span", async () => {
  for (const rule of gridColumnRules(await chartGridCss())) {
    assert.ok(rule.includes(LG), `grid-column set below lg: ${rule.join(" ")}`);
  }
  // Nothing in the classes reaches below lg.
  for (const name of OVERVIEW_CHART_COLUMNS.split(/\s+/)) assert.match(name, /^lg:/);
});
