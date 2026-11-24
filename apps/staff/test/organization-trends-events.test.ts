import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { OrganizationEventTableCard } from "../app/organization-trends-events.tsx";
import { OrganizationTrendsReading } from "../app/organization-trends-section.tsx";
import type { OrganizationTrendEvent, OrganizationTrendFigure, OrganizationTrends } from "../lib/organization-trends.ts";

// The Event table of Organization Trends (#865) as Home draws it beneath the
// charts: ranked by Takings for an Org Admin and an Event Owner, by Tickets
// Sold with no Takings column for Event Staff, each row linking into its
// Event's Sales Trends and Reach. The rows are handed out of order, so the
// order drawn is the table's own.

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

function figure(values: number[]): OrganizationTrendFigure {
  const total = values.reduce((sum, value) => sum + value, 0);
  return {
    total,
    comparison_total: null,
    change: null,
    series: [
      {
        key: "all",
        total,
        buckets: values.map((value, index) => ({ first_day: days[index], last_day: days[index], value, partial: false })),
        today: null,
        comparison_total: null,
        change: null,
        comparison_buckets: [],
      },
    ],
  };
}

const adminEvents: OrganizationTrendEvent[] = [
  {
    event_id: "gamma",
    name: "Gamma Fest",
    starts_at: null,
    timezone: "",
    tickets_sold: 0,
    takings_cents: 0,
    page_views: 40,
  },
  {
    event_id: "beta",
    name: "Beta Fest",
    starts_at: "2026-07-04T20:00:00Z",
    timezone: "America/Guayaquil",
    tickets_sold: 9,
    takings_cents: 1000,
    page_views: 1,
  },
  {
    event_id: "alpha",
    name: "Alpha Fest",
    starts_at: "2026-07-02T23:00:00Z",
    timezone: "America/Guayaquil",
    tickets_sold: 2,
    takings_cents: 9000,
    page_views: 5,
  },
];

function reading(role: "orgAdmin" | "eventOwner" | "eventStaff"): OrganizationTrends {
  const seesTakings = role !== "eventStaff";
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
    comparison: null,
    granularity: "day",
    figures: {
      tickets_sold: figure([2, 0, 9]),
      ticket_sales: figure([1, 0, 2]),
      page_views: figure([40, 5, 1]),
      events_held: figure([0, 0, 1]),
      ...(seesTakings ? { takings: figure([9000, 0, 1000]) } : {}),
    },
    events: seesTakings
      ? adminEvents
      : adminEvents.map(({ takings_cents: _withheld, ...event }) => event),
  };
}

function renderTable(trends: OrganizationTrends, locale: "en" | "es" = "en"): string {
  return render(React.createElement(OrganizationEventTableCard, { trends, locale }), locale);
}

/** The Events' names in the order the table draws them. */
function order(markup: string): string[] {
  const words = text(markup);
  return ["Alpha Fest", "Beta Fest", "Gamma Fest"].sort((a, b) => words.indexOf(a) - words.indexOf(b));
}

/** The table's column headers. */
function headers(markup: string): string[] {
  return [...markup.matchAll(/<th scope="col"[^>]*>([^<]*)<\/th>/g)].map((match) => match[1]);
}

for (const role of ["orgAdmin", "eventOwner"] as const) {
  test(`for ${role === "orgAdmin" ? "an Org Admin" : "an Event Owner"} the table shows Takings and ranks by it`, () => {
    const markup = renderTable(reading(role));
    assert.deepEqual(headers(markup), ["Event", "Starts", "Takings", "Tickets sold", "Page views"]);
    assert.deepEqual(order(markup), ["Alpha Fest", "Beta Fest", "Gamma Fest"]);
    assert.match(text(markup), /Alpha Fest .*\$90\.00 2 5 /);
    assert.match(text(markup), /ranked by takings/);
  });
}

test("for Event Staff the table ranks by Tickets Sold with no Takings column and no money", () => {
  const markup = renderTable(reading("eventStaff"));
  assert.deepEqual(headers(markup), ["Event", "Starts", "Tickets sold", "Page views"]);
  assert.deepEqual(order(markup), ["Beta Fest", "Alpha Fest", "Gamma Fest"]);
  assert.doesNotMatch(markup, /Takings|\$/);
  assert.match(text(markup), /ranked by tickets sold/);
});

test("each row links to its Event's Sales Trends and Reach, named by the Event", () => {
  for (const role of ["orgAdmin", "eventStaff"] as const) {
    const markup = renderTable(reading(role));
    for (const [id, name] of [
      ["alpha", "Alpha Fest"],
      ["beta", "Beta Fest"],
      ["gamma", "Gamma Fest"],
    ]) {
      assert.match(markup, new RegExp(`<a aria-label="Sales Trends of ${name}"[^>]*href="/events/${id}/sales/trends"`), `${role} ${id}`);
      assert.match(markup, new RegExp(`<a aria-label="Reach of ${name}"[^>]*href="/events/${id}/reach"`), `${role} ${id}`);
    }
  }
});

test("each row carries its start date, and says when an Event names none", () => {
  const words = text(renderTable(reading("orgAdmin")));
  assert.match(words, /Alpha Fest Thu, Jul 2, 2026/);
  assert.match(words, /Gamma Fest No date set/);
});

test("the table sits beneath the charts on Home", () => {
  const markup = render(React.createElement(OrganizationTrendsReading, { trends: reading("orgAdmin"), locale: "en" }));
  const words = text(markup);
  assert.ok(words.indexOf("Events in the span") > words.lastIndexOf("Events held, day by day"), "beneath the last chart");
});

test("a span nothing happened in says so instead of drawing an empty table", () => {
  const markup = renderTable({ ...reading("orgAdmin"), events: [] });
  assert.doesNotMatch(markup, /<table/);
  assert.match(text(markup), /No Event was held, loaded or sold in this span\./);
});

test("the table reads in Ecuadorian Spanish", () => {
  const markup = renderTable(reading("orgAdmin"), "es");
  assert.deepEqual(headers(markup), ["Evento", "Inicio", "Recaudación", "Entradas vendidas", "Vistas de página"]);
  assert.match(text(markup), /Eventos del periodo/);
  assert.match(markup, /aria-label="Tendencias de ventas de Alpha Fest"/);
  assert.match(markup, /aria-label="Alcance de Alpha Fest"/);
  assert.match(text(markup), /Gamma Fest Sin fecha/);
  const staff = renderTable(reading("eventStaff"), "es");
  assert.match(text(staff), /ordenado por entradas vendidas/);
  assert.doesNotMatch(staff, /Recaudación/);
});
