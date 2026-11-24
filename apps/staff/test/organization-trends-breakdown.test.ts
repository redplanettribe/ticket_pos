import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { OrganizationBreakdownReading } from "../app/organization-trends-breakdown.tsx";
import { OrganizationTrendsReading } from "../app/organization-trends-section.tsx";
import { formatMoney, formatNumber } from "../lib/format.ts";
import {
  organizationBreakdownView,
  type OrganizationTrendBreakdown,
  type OrganizationTrendBucketSelection,
} from "../lib/organization-trends-breakdown.ts";
import type { OrganizationTrendFigure, OrganizationTrends } from "../lib/organization-trends.ts";

// The breakdown of Organization Trends (#864): a bucket chosen on a Daily
// chart opens, beneath that chart, into the Events that made it, ranked, each
// linking to that Event. Recharts draws no marks on a server render, so the
// choice is handed to the section as its opening selection, and the Events as
// the reading the panel draws once its request is answered.

const catalogs = {
  en: JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8")),
  es: JSON.parse(readFileSync(new URL("../messages/es.json", import.meta.url), "utf8")),
};

function render(node: React.ReactNode, locale: "en" | "es" = "en"): string {
  return renderToStaticMarkup(
    React.createElement(NextIntlClientProvider, { locale, messages: catalogs[locale], timeZone: "UTC", children: node }),
  );
}

function text(markup: string): string {
  return markup.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
}

const days = ["2026-06-30", "2026-07-01", "2026-07-02"];

function figure(values: Record<string, number[]>): OrganizationTrendFigure {
  const series = Object.entries(values).map(([key, entries]) => ({
    key,
    total: entries.reduce((sum, value) => sum + value, 0),
    buckets: entries.map((value, index) => ({ first_day: days[index], last_day: days[index], value, partial: false })),
    today: { first_day: "2026-07-03", last_day: "2026-07-03", value: 0, partial: true },
    comparison_total: 0,
    change: null,
    comparison_buckets: entries.map(() => ({ first_day: "x", last_day: "x", value: 0, partial: false })),
  }));
  return { total: series.reduce((sum, entry) => sum + entry.total, 0), comparison_total: 0, change: null, series };
}

function reading(overrides: Partial<OrganizationTrends["figures"]> = {}): OrganizationTrends {
  return {
    timezone: "America/Guayaquil",
    today: "2026-07-03",
    currency: "USD",
    span: {
      preset: "custom",
      first_day: days[0],
      last_day: days[2],
      buckets: days.map((day) => ({ first_day: day, last_day: day, partial: false })),
    },
    comparison: { kind: "previous", first_day: "2026-06-27", last_day: "2026-06-29" },
    granularity: "day",
    figures: {
      tickets_sold: figure({ online: [2, 0, 3], in_person: [0, 0, 0], import: [5, 0, 0] }),
      ticket_sales: figure({ all: [2, 0, 1] }),
      page_views: figure({ all: [40, 0, 25] }),
      events_held: figure({ all: [1, 0, 1] }),
      takings: figure({ online: [1000, 0, 500], in_person: [0, 0, 0], import: [0, 0, 0] }),
      ...overrides,
    },
  };
}

const july2 = (figure: OrganizationTrendBucketSelection["figure"]): OrganizationTrendBucketSelection => ({
  figure,
  key: "2026-07-02",
  firstDay: "2026-07-02",
  lastDay: "2026-07-02",
});

function renderSection(
  trends: OrganizationTrends,
  initialSelection: OrganizationTrendBucketSelection | null,
  locale: "en" | "es" = "en",
  initialChartView: "daily" | "cumulative" = "daily",
) {
  return render(React.createElement(OrganizationTrendsReading, { trends, locale, initialChartView, initialSelection }), locale);
}

/** The words of the card titled `title`, up to the next card. */
function card(markup: string, title: string): string {
  const words = text(markup);
  const start = words.indexOf(` ${title} `, words.indexOf("Daily"));
  assert.ok(start >= 0, `a card titled ${title}`);
  return words.slice(start);
}

test("choosing a day on a chart opens the panel beneath that chart, named by the figure and the day", () => {
  const markup = renderSection(reading(), july2("tickets_sold"));
  const words = text(markup);
  assert.match(words, /Tickets sold, Jul 2: the Events that made it/);
  assert.match(markup, /role="status" aria-label="Loading the Events\.\.\."/);
  // Beneath Tickets Sold, before the Page Views card.
  const ticketsAt = words.indexOf("Tickets sold, Jul 2");
  assert.ok(ticketsAt > words.lastIndexOf("Tickets sold, day by day"), "the panel sits under its chart");
  assert.ok(ticketsAt < words.indexOf("Page views", ticketsAt - 0), "and above the next chart");
  assert.equal((words.match(/the Events that made it/g) ?? []).length, 1, "one panel for the section");
  assert.ok(card(markup, "Tickets sold").includes("Close"));
});

test("each Daily chart's buckets may be chosen", () => {
  const markup = renderSection(reading(), null);
  for (const figure of ["Takings", "Tickets sold", "Page views", "Events held"]) {
    assert.match(markup, new RegExp(`role="listbox"[^>]*aria-label="${figure}: choose a day to see the Events that made it"`), figure);
  }
  // Each bucket is read aloud by its day and its figure.
  assert.match(text(markup), /Tickets sold .* Jul 2: 3 Jul 3: 0, partial/);
  assert.doesNotMatch(text(markup), /: the Events that made it/, "nothing is open until a bucket is chosen");
});

test("a month span offers months, and the Spanish catalog words it", () => {
  const months = { ...reading(), granularity: "month" as const };
  assert.match(renderSection(months, null), /aria-label="Page views: choose a month to see the Events that made it"/);
  assert.match(renderSection(months, null, "es"), /aria-label="Vistas de página: elija un mes para ver los eventos que lo formaron"/);
});

test("the Cumulative view opens nothing: a running sum is no Event's alone", () => {
  const words = text(renderSection(reading(), july2("tickets_sold"), "en", "cumulative"));
  assert.doesNotMatch(words, /the Events that made it/);
});

test("Event Staff are handed no Takings, so no Takings bucket opens", () => {
  const words = text(renderSection(reading({ takings: null }), july2("takings")));
  assert.doesNotMatch(words, /Takings/);
  assert.doesNotMatch(words, /the Events that made it/);
});

function breakdown(patch: Partial<OrganizationTrendBreakdown>): OrganizationTrendBreakdown {
  return {
    timezone: "America/Guayaquil",
    figure: "tickets_sold",
    granularity: "day",
    first_day: "2026-07-02",
    last_day: "2026-07-02",
    partial: false,
    total: 10,
    events: [
      { event_id: "e1", name: "Big Fest", value: 5 },
      { event_id: "e3", name: "Import Fest", value: 4 },
      { event_id: "e2", name: "Small Fest", value: 1 },
    ],
    ...patch,
  };
}

function renderBreakdown(reading: OrganizationTrendBreakdown, formatValue: (value: number) => string, locale: "en" | "es" = "en") {
  return render(
    React.createElement(OrganizationBreakdownReading, { view: organizationBreakdownView(reading), formatValue }),
    locale,
  );
}

const count = (value: number) => formatNumber(value, "en");

test("the bucket's Events are shown ranked, each with its figure and linking to that Event", () => {
  const markup = renderBreakdown(breakdown({}), count);
  assert.match(markup, /<ol[^>]*list-decimal/);
  const links = [...markup.matchAll(/<a[^>]*href="([^"]+)"[^>]*>([^<]+)<\/a>/g)].map((match) => `${match[2]} ${match[1]}`);
  assert.deepEqual(links, ["Big Fest /events/e1", "Import Fest /events/e3", "Small Fest /events/e2"]);
  assert.match(text(markup), /Total 10 Events Big Fest 5 Import Fest 4 Small Fest 1/);
});

test("Takings is written in the Organization's currency", () => {
  const money = (cents: number) => formatMoney(cents, "USD", "en");
  const words = text(
    renderBreakdown(
      breakdown({ figure: "takings", total: 1500, events: [{ event_id: "e1", name: "Big Fest", value: 1500 }] }),
      money,
    ),
  );
  assert.match(words, new RegExp(`Big Fest ${money(1500).replace(/[$.]/g, "\\$&")}`));
});

test("Events held is a list of the Events, each counting one", () => {
  const markup = renderBreakdown(
    breakdown({
      figure: "events_held",
      total: 2,
      events: [
        { event_id: "e1", name: "Big Fest", value: 1 },
        { event_id: "e2", name: "Small Fest", value: 1 },
      ],
    }),
    count,
  );
  assert.match(markup, /<ul/);
  assert.doesNotMatch(markup, /<ol/);
  assert.match(text(markup), /Total 2 Events Big Fest Small Fest/);
});

test("a quiet bucket says no Event contributed, and today says it can still grow, in both locales", () => {
  const empty = breakdown({ total: 0, events: [], partial: true, first_day: "2026-07-03", last_day: "2026-07-03" });
  const en = text(renderBreakdown(empty, count));
  assert.match(en, /No Event contributed to this figure here\./);
  assert.match(en, /Today so far/);
  const es = text(renderBreakdown(empty, count, "es"));
  assert.match(es, /Ningún evento aportó a esta cifra aquí\./);
  assert.match(es, /En lo que va de hoy/);
});
