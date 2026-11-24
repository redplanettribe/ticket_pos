import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import {
  ORGANIZATION_TRENDS_TILES_GRID,
  OrganizationTrendsPicker,
  OrganizationTrendsReading,
} from "../app/organization-trends-section.tsx";
import type {
  OrganizationTrendFigure,
  OrganizationTrends,
  OrganizationTrendsView,
} from "../lib/organization-trends.ts";

// Organization Trends (#861) as Home draws it: the tiles, the Tickets Sold
// chart in its two views, the partial marking, the zero span and the picker.
// The API states every number; these assert the page draws what it is handed.

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

function figure(perChannel: Record<string, number[]>, today: Record<string, number>, compared: number | null): OrganizationTrendFigure {
  const series = Object.entries(perChannel).map(([key, values]) => ({
    key,
    total: values.reduce((sum, value) => sum + value, 0),
    buckets: values.map((value, index) => ({ first_day: days[index], last_day: days[index], value, partial: false })),
    today: { first_day: "2026-07-03", last_day: "2026-07-03", value: today[key] ?? 0, partial: true },
    comparison_total: compared === null ? null : 0,
    change: null,
    comparison_buckets: compared === null ? [] : values.map(() => ({ first_day: "x", last_day: "x", value: 0, partial: false })),
  }));
  const total = series.reduce((sum, entry) => sum + entry.total, 0);
  return {
    total,
    comparison_total: compared,
    change: compared === null || compared === 0 ? null : (total - compared) / compared,
    series,
  };
}

function reading(overrides: Partial<OrganizationTrends> = {}): OrganizationTrends {
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
      tickets_sold: figure({ online: [2, 0, 3], in_person: [0, 0, 0], import: [5, 0, 0] }, { online: 4 }, 5),
      ticket_sales: figure({ all: [2, 0, 1] }, { all: 1 }, 3),
    },
    ...overrides,
  };
}

function renderReading(trends: OrganizationTrends, locale: "en" | "es" = "en", initialChartView: "daily" | "cumulative" = "daily") {
  return render(React.createElement(OrganizationTrendsReading, { trends, locale, initialChartView }), locale);
}

test("a tile per figure states its total and its change against the comparison span", () => {
  const words = text(renderReading(reading()));
  assert.match(words, /Tickets sold 10 /);
  assert.match(words, /\+100%/);
  assert.match(words, /Ticket sales 3 /);
  assert.match(words, /Compared with/);
});

test("the tiles stack in one column on a phone", () => {
  assert.match(ORGANIZATION_TRENDS_TILES_GRID, /(^| )grid-cols-1( |$)/);
  assert.match(renderReading(reading()), new RegExp(`class="${ORGANIZATION_TRENDS_TILES_GRID}"`));
});

test("the Tickets Sold chart is stacked by Sales Channel, with a Daily and a Cumulative view", () => {
  const markup = renderReading(reading());
  const words = text(markup);
  assert.match(words, /Online.*In person.*Import/);
  assert.match(markup, /role="radiogroup"[^>]*aria-label="How the buckets are counted"/);
  assert.match(words, /Daily/);
  assert.match(words, /Cumulative/);
  assert.match(markup, /aria-label="Tickets sold, day by day"/);
  assert.match(words, /stacked by Sales Channel/);

  const cumulative = renderReading(reading(), "en", "cumulative");
  assert.match(cumulative, /aria-label="Tickets sold since the start of the span, day by day"/);
});

test("today is said to be partial and in no total", () => {
  const words = text(renderReading(reading()));
  assert.match(words, /Today is drawn faint, as partial, and is left out of every total and comparison/);
  // Today's 4 is in neither the tile nor the total.
  assert.doesNotMatch(words, /Tickets sold 14/);
});

test("month buckets are read by month, and all time states no change", () => {
  const months = reading({
    granularity: "month",
    comparison: null,
    span: {
      preset: "all",
      first_day: "2026-05-01",
      last_day: "2026-07-02",
      buckets: [
        { first_day: "2026-05-01", last_day: "2026-05-31", partial: false },
        { first_day: "2026-06-01", last_day: "2026-06-30", partial: false },
        { first_day: "2026-07-01", last_day: "2026-07-02", partial: true },
      ],
    },
    figures: {
      tickets_sold: figure({ online: [1, 2, 3] }, {}, null),
      ticket_sales: figure({ all: [1, 1, 1] }, {}, null),
    },
  });
  const markup = renderReading(months);
  const words = text(markup);
  assert.match(markup, /aria-label="Tickets sold, month by month"/);
  assert.match(words, /by month\./);
  assert.doesNotMatch(words, /Compared with/);
  assert.doesNotMatch(words, /Nothing to compare against/);
  assert.doesNotMatch(words, /%/);
});

test("a span with no sales reads as zeros, and says nothing was sold", () => {
  const empty = reading({
    figures: {
      tickets_sold: figure({ online: [0, 0, 0], in_person: [0, 0, 0], import: [0, 0, 0] }, {}, 0),
      ticket_sales: figure({ all: [0, 0, 0] }, {}, 0),
    },
  });
  const words = text(renderReading(empty));
  assert.match(words, /Tickets sold 0 /);
  assert.match(words, /Ticket sales 0 /);
  assert.match(words, /None in the span compared/);
  assert.match(words, /Nothing was sold in this span\./);
  assert.doesNotMatch(text(renderReading(reading())), /Nothing was sold/);
});

test("the reading is in Ecuadorian Spanish under the Spanish locale", () => {
  const words = text(renderReading(reading(), "es"));
  assert.match(words, /Entradas vendidas 10 /);
  assert.match(words, /Ventas de entradas 3 /);
  assert.match(words, /En línea.*Presencial.*Importación/);
  assert.match(words, /Diario/);
  assert.match(words, /Acumulado/);
});

function renderPicker(view: OrganizationTrendsView, locale: "en" | "es" = "en") {
  return render(
    React.createElement(OrganizationTrendsPicker, { view, shown: null, waiting: false, onShow: () => {} }),
    locale,
  );
}

test("the picker offers every span and both comparisons, and all time disables the comparison", () => {
  const markup = renderPicker({ span: "30d", from: "", to: "", compare: "year" });
  const words = text(markup);
  for (const span of ["Last 7 days", "Last 30 days", "Last 90 days", "Last 12 months", "All time", "Custom range"]) {
    assert.match(words, new RegExp(span));
  }
  assert.match(words, /The span before/);
  assert.match(words, /The same span a year earlier/);
  assert.doesNotMatch(markup, /id="organization-trends-compare"[^>]*disabled=""/);

  const allTime = renderPicker({ span: "all", from: "", to: "", compare: "previous" });
  assert.match(allTime, /id="organization-trends-compare"[^>]*disabled=""/);
  assert.match(text(allTime), /all time has no span before it/);

  const custom = renderPicker({ span: "custom", from: "2026-01-15", to: "2026-06-10", compare: "previous" });
  assert.match(custom, /id="organization-trends-from"[^>]*value="2026-01-15"/);
  assert.match(text(renderPicker({ span: "all", from: "", to: "", compare: "previous" }, "es")), /Desde el inicio/);
});

// Takings (#862): the API sends it to an Org Admin and an Event Owner and
// withholds it from Event Staff, so the reading each is handed differs by
// whether figures.takings is there. The page draws Takings only when it is.

const takingsFigure = () =>
  figure({ online: [2000, 0, 3000], in_person: [0, 0, 0], import: [7500, 0, 0] }, { online: 4000 }, 6250);

const handedTo = {
  orgAdmin: () => reading({ figures: { ...reading().figures, takings: takingsFigure() } }),
  eventOwner: () => reading({ figures: { ...reading().figures, takings: takingsFigure() } }),
  eventStaffNull: () => reading({ figures: { ...reading().figures, takings: null } }),
  eventStaffOmitted: () => reading(),
};

for (const role of ["orgAdmin", "eventOwner"] as const) {
  test(`an ${role === "orgAdmin" ? "Org Admin" : "Event Owner"} sees the Takings tile and the Takings chart`, () => {
    const markup = renderReading(handedTo[role]());
    const words = text(markup);
    // 20.00 + 30.00 + 75.00, today's 40.00 in no total, against 62.50.
    assert.match(words, /Takings \$125\.00 /);
    assert.match(words, /\+100%/);
    assert.match(markup, /aria-label="Takings, day by day"/);
    assert.match(words, /on every Sales Channel/);
    const cumulative = renderReading(handedTo[role](), "en", "cumulative");
    assert.match(cumulative, /aria-label="Takings since the start of the span, day by day"/);
  });
}

for (const handed of ["eventStaffNull", "eventStaffOmitted"] as const) {
  test(`Event Staff see neither the Takings tile nor the Takings chart (${handed === "eventStaffNull" ? "null" : "omitted"})`, () => {
    const markup = renderReading(handedTo[handed]());
    const words = text(markup);
    assert.doesNotMatch(words, /Takings/);
    assert.doesNotMatch(words, /\$/);
    assert.doesNotMatch(markup, /aria-label="Takings/);
    // The counts are still there.
    assert.match(words, /Tickets sold 10 /);
    assert.match(markup, /aria-label="Tickets sold, day by day"/);
  });
}

test("Takings reads in Ecuadorian Spanish, in the Organization's currency", () => {
  const words = text(renderReading(handedTo.orgAdmin(), "es"));
  assert.match(words, /Recaudación \$125,00 /);
  assert.match(words, /en todos los canales de venta/);
});
