import assert from "node:assert/strict";
import test from "node:test";

import type { OrganizationTrendEvent, OrganizationTrends } from "./organization-trends.ts";
import { eventReachHref, eventSalesTrendsHref, organizationEventTable } from "./organization-trends-events.ts";

const figure = { total: 0, comparison_total: null, change: null, series: [] };

function reading(events: OrganizationTrendEvent[], seesTakings: boolean): OrganizationTrends {
  return {
    timezone: "America/Guayaquil",
    today: "2026-07-07",
    currency: "USD",
    span: { preset: "7d", first_day: "2026-06-30", last_day: "2026-07-06", buckets: [] },
    comparison: null,
    granularity: "day",
    figures: {
      tickets_sold: figure,
      ticket_sales: figure,
      page_views: figure,
      events_held: figure,
      ...(seesTakings ? { takings: figure } : { takings: null }),
    },
    events,
  };
}

function event(patch: Partial<OrganizationTrendEvent>): OrganizationTrendEvent {
  return { event_id: "e", name: "E", starts_at: null, timezone: "", tickets_sold: 0, takings_cents: null, page_views: 0, ...patch };
}

const handed = [
  event({ event_id: "c", name: "Gamma Fest", tickets_sold: 0, takings_cents: 0, page_views: 40 }),
  event({ event_id: "b", name: "Beta Fest", tickets_sold: 9, takings_cents: 1000, page_views: 1 }),
  event({ event_id: "a", name: "Alpha Fest", tickets_sold: 2, takings_cents: 9000, page_views: 5, timezone: "Europe/Madrid" }),
];

test("a reading with Takings shows a Takings column and ranks by it", () => {
  const table = organizationEventTable(reading(handed, true));
  assert.equal(table.showsTakings, true);
  assert.deepEqual(
    table.rows.map((row) => [row.name, row.takingsCents]),
    [
      ["Alpha Fest", 9000],
      ["Beta Fest", 1000],
      ["Gamma Fest", 0],
    ],
  );
});

test("a reading without Takings has no Takings column and ranks by Tickets Sold, then Page Views", () => {
  const staffEvents = handed.map((row) => ({ ...row, takings_cents: null }));
  const table = organizationEventTable(reading(staffEvents, false));
  assert.equal(table.showsTakings, false);
  assert.deepEqual(
    table.rows.map((row) => [row.name, row.takingsCents]),
    [
      ["Beta Fest", null],
      ["Alpha Fest", null],
      ["Gamma Fest", null],
    ],
  );
  const tie = organizationEventTable(
    reading([event({ event_id: "y", name: "Zed", page_views: 1 }), event({ event_id: "x", name: "Ann", page_views: 3 })], false),
  );
  assert.deepEqual(
    tie.rows.map((row) => row.name),
    ["Ann", "Zed"],
  );
});

test("each row links to its Event's Sales Trends and Reach, and prints its start in its own zone", () => {
  const [alpha] = organizationEventTable(reading(handed, true)).rows;
  assert.equal(alpha.salesTrendsHref, "/events/a/sales/trends");
  assert.equal(alpha.reachHref, "/events/a/reach");
  assert.equal(alpha.timeZone, "Europe/Madrid");
  assert.equal(organizationEventTable(reading(handed, true)).rows[2].timeZone, "America/Guayaquil");
  assert.equal(eventSalesTrendsHref("e/2"), "/events/e%2F2/sales/trends");
  assert.equal(eventReachHref("e/2"), "/events/e%2F2/reach");
});
