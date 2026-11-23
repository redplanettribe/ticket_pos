import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { HOME_NOW_GRID, HomeNowStrip } from "../app/home-now-strip.tsx";
import type { HomeNow } from "../lib/home-now.ts";

// Home's "now" strip (#859) as each role is handed it. The API withholds the
// money (ADR 0081); these assert the page draws exactly what it was handed,
// and never a zero where a figure is absent.

const catalogs = {
  en: JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8")),
  es: JSON.parse(readFileSync(new URL("../messages/es.json", import.meta.url), "utf8")),
};

const NOW = Date.parse("2026-07-07T12:00:00Z");

function render(home: HomeNow, role: string, locale: "en" | "es" = "en"): string {
  return renderToStaticMarkup(
    React.createElement(NextIntlClientProvider, {
      locale,
      messages: catalogs[locale],
      timeZone: "UTC",
      children: React.createElement(HomeNowStrip, { home, role, now: NOW }),
    }),
  );
}

/** The markup as the text a reader sees. */
function text(markup: string): string {
  return markup.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
}

const base: HomeNow = {
  timezone: "America/Guayaquil",
  today: "2026-07-07",
  currency: "USD",
  has_events: true,
  active_events: 4,
  next_event: {
    id: "evt-1",
    name: "Summer Fest",
    starts_at: "2026-07-10T01:00:00Z",
    timezone: "America/Guayaquil",
    tickets_sold: 120,
    ongoing: false,
  },
  today_so_far: { partial: true, tickets_sold: 7 },
};

// What each role is handed, as the API states it.
const asOrgAdmin: HomeNow = {
  ...base,
  today_so_far: { ...base.today_so_far, takings_cents: 12345 },
  payable_balance_cents: 50000,
};
const asEventOwner: HomeNow = { ...base, today_so_far: { ...base.today_so_far, takings_cents: 12345 }, payable_balance_cents: null };
const asEventStaff: HomeNow = { ...base, today_so_far: { ...base.today_so_far, takings_cents: null }, payable_balance_cents: null };

test("an Org Admin sees every count, today's Takings and the Payable Balance linking to Payouts", () => {
  const markup = render(asOrgAdmin, "org_admin");
  const words = text(markup);
  assert.match(words, /Active events 4 See events/);
  assert.match(markup, /href="\/events"/);
  assert.match(words, /Summer Fest/);
  assert.match(markup, /href="\/events\/evt-1"/);
  assert.match(words, /120 tickets sold so far/);
  assert.match(words, /Starts in 2 days/);
  assert.match(words, /Today so far Partial/);
  assert.match(words, /Tickets sold 7/);
  assert.match(words, /Takings \$123\.45/);
  assert.match(words, /Payable balance \$500\.00 Go to Payouts/);
  assert.match(markup, /href="\/payouts"/);
});

test("an Event Owner sees today's Takings and no Payable Balance", () => {
  const words = text(render(asEventOwner, "event_owner"));
  assert.match(words, /Takings \$123\.45/);
  assert.doesNotMatch(words, /Payable balance/);
  assert.doesNotMatch(words, /Go to Payouts/);
});

test("Event Staff see every count and no money at all", () => {
  const markup = render(asEventStaff, "event_staff");
  const words = text(markup);
  assert.match(words, /Active events 4/);
  assert.match(words, /120 tickets sold so far/);
  assert.match(words, /Tickets sold 7/);
  assert.doesNotMatch(words, /Takings/);
  assert.doesNotMatch(words, /Payable balance/);
  assert.doesNotMatch(words, /\$/);
});

test("an Ongoing next Event is marked Ongoing, with no countdown", () => {
  const words = text(
    render({ ...asOrgAdmin, next_event: { ...base.next_event!, starts_at: "2026-07-06T12:00:00Z", ongoing: true } }, "org_admin"),
  );
  assert.match(words, /Next event Ongoing/);
  assert.doesNotMatch(words, /Starts /);
});

test("with no next Event the card says so", () => {
  const words = text(render({ ...asOrgAdmin, next_event: null }, "org_admin"));
  assert.match(words, /No published event still to start or running\./);
});

test("an Organization with no Events says so, and points an Org Admin to creating the first", () => {
  const empty: HomeNow = { ...asOrgAdmin, has_events: false, active_events: 0, next_event: null };
  const admin = render(empty, "org_admin");
  assert.match(text(admin), /No events yet/);
  assert.match(admin, /href="\/events\/new"/);
  assert.doesNotMatch(text(admin), /Active events/);

  for (const role of ["event_owner", "event_staff"]) {
    const markup = render({ ...empty, payable_balance_cents: null }, role);
    assert.match(text(markup), /No events yet/);
    assert.doesNotMatch(markup, /href="\/events\/new"/);
  }
});

test("Home reads in Ecuadorian Spanish", () => {
  const words = text(render(asOrgAdmin, "org_admin", "es"));
  assert.match(words, /Eventos activos/);
  assert.match(words, /Empieza dentro de 2 días/);
  assert.match(words, /Hoy hasta ahora Parcial/);
  assert.match(words, /Recaudación/);
  assert.match(words, /Saldo pagable/);
});

test("on a phone the cards stack in one column", () => {
  const classes = HOME_NOW_GRID.split(/\s+/);
  assert.ok(classes.includes("grid-cols-1"), HOME_NOW_GRID);
  // Every other column count waits for a breakpoint.
  for (const name of classes.filter((c) => /grid-cols-/.test(c) && c !== "grid-cols-1")) {
    assert.match(name, /^(sm|md|lg|xl):/, name);
  }
});
