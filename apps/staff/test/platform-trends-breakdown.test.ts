import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { BreakdownReading } from "../app/operator/platform-trends-breakdown.tsx";
import { breakdownView, type PlatformTrendBreakdown } from "../lib/platform-trends-breakdown.ts";

const messages = JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8"));

function breakdown(patch: Partial<PlatformTrendBreakdown>): PlatformTrendBreakdown {
  return {
    timezone: "America/Guayaquil",
    figure: "tickets_sold",
    granularity: "day",
    first_day: "2026-07-06",
    last_day: "2026-07-06",
    partial: false,
    recorded: true,
    sale_half_only: false,
    totals: [{ key: "all", value: 7 }],
    organizations: [],
    events: [],
    customer_kinds: null,
    ...patch,
  };
}

const organization = (id: string, value: number) => ({
  organization_id: id,
  name: `Organization ${id}`,
  key: "all",
  value,
  cause: null,
});

const event = (id: string, value: number) => ({
  event_id: id,
  name: `Event ${id}`,
  organization_id: "o1",
  organization_name: "Organization o1",
  key: "all",
  value,
});

function render(reading: PlatformTrendBreakdown): string {
  return renderToStaticMarkup(
    React.createElement(NextIntlClientProvider, {
      locale: "en",
      messages,
      timeZone: "America/Guayaquil",
      children: React.createElement(BreakdownReading, { view: breakdownView(reading), locale: "en" }),
    }),
  );
}

/** The class lists of every element in the markup. */
const classesOf = (markup: string) => [...markup.matchAll(/class="([^"]*)"/g)].map((match) => match[1].split(/\s+/));

/** The two-column classes among `classes`, at any breakpoint of the
 * viewport or of a container. */
const twoColumnClasses = (classes: string[]) => classes.filter((name) => /(^|:)grid-cols-2$/.test(name));

/** Whether any element is told to lay itself out in two columns. */
const twoColumns = (markup: string) => classesOf(markup).some((classes) => twoColumnClasses(classes).length > 0);

/** A container query of the panel, as `@lg:`, not a viewport one, as `sm:`. */
const CONTAINER_QUERY = /^@[a-z0-9]+:/;

// --- the lists follow the panel and what is in it, not the viewport (#834) --

test("Page Views' single Events list takes the panel's whole width", () => {
  const markup = render(breakdown({ figure: "page_views", events: [event("e1", 5), event("e2", 2)] }));
  assert.match(markup, />Events</, "the Events list is drawn");
  assert.doesNotMatch(markup, />Organizations</, "Page Views lists no Organizations");
  assert.equal(twoColumns(markup), false, "one list is not laid out in two columns");
});

test("two lists sit side by side only where the panel is wide enough, never by the viewport", () => {
  const markup = render(breakdown({ organizations: [organization("o1", 7)], events: [event("e1", 7)] }));
  const grids = classesOf(markup).map(twoColumnClasses).filter((columns) => columns.length > 0);
  assert.equal(grids.length, 1, "the lists' grid pairs them up");
  assert.deepEqual(
    grids[0].map((name) => CONTAINER_QUERY.test(name)),
    [true],
    `a container query of the panel, not a viewport breakpoint: ${grids[0].join(" ")}`,
  );
});

test("a lone total of a count takes the panel's whole width", () => {
  const markup = render(
    breakdown({ figure: "new_verified_customers", totals: [{ key: "all", value: 3 }], customer_kinds: null }),
  );
  assert.match(markup, /<dl/);
  assert.equal(twoColumns(markup), false, "one term is not laid out in two columns");
});

test("the kinds of Active Customers pair up by the panel's width, not the viewport's", () => {
  const markup = render(
    breakdown({
      figure: "active_customers",
      totals: [{ key: "all", value: 9 }],
      customer_kinds: { new: 4, returning: 5, returning_bought: 2 },
    }),
  );
  const dl = markup.match(/<dl class="([^"]*)"/)?.[1].split(/\s+/) ?? [];
  assert.ok(dl.includes("grid"), `the terms are a grid: ${dl.join(" ")}`);
  assert.deepEqual(
    twoColumnClasses(dl).map((name) => CONTAINER_QUERY.test(name)),
    [true],
    `a container query of the panel, not a viewport breakpoint: ${dl.join(" ")}`,
  );
});
