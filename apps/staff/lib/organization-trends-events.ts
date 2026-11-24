// The Event table of Organization Trends (#865, spec #858): every Event held,
// loaded or sold in the span, beneath the charts, each row linking into that
// Event's Sales Trends and Reach.
//
// The API states every number and ranks the rows, and withholds Takings from
// Event Staff. What is here is the little the page does with that: whether
// the table has a Takings column (the reading carries Takings), the order it
// is drawn in, and each row's links. The order is the API's ranking (by
// Takings when the table has a Takings column, by Tickets Sold when it has
// not), set again here so the column ranked by is always the column shown.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import type { OrganizationTrendEvent, OrganizationTrends } from "./organization-trends.ts";

/** One row of the table as the page draws it. */
export type OrganizationEventTableRow = {
  id: string;
  name: string;
  startsAt: string | null;
  /** The zone the start is printed in: the Event's own, else the platform's. */
  timeZone: string;
  ticketsSold: number;
  /** Null when the table has no Takings column. */
  takingsCents: number | null;
  pageViews: number;
  salesTrendsHref: string;
  reachHref: string;
};

/** The table: whether it shows Takings, and its rows in the order drawn. */
export type OrganizationEventTable = {
  showsTakings: boolean;
  rows: OrganizationEventTableRow[];
};

/** An Event's Sales Trends, the Sales surface's Trends tab. */
export function eventSalesTrendsHref(eventId: string): string {
  return `/events/${encodeURIComponent(eventId)}/sales/trends`;
}

/** An Event's Reach, whose index is Reach Trends. */
export function eventReachHref(eventId: string): string {
  return `/events/${encodeURIComponent(eventId)}/reach`;
}

/**
 * The Event table of a reading. It shows Takings exactly when the reading
 * carries Takings, which the API hands an Org Admin and an Event Owner and
 * withholds from Event Staff, and is then ranked by Takings; without it, by
 * Tickets Sold. Ties fall to Page Views, then name, as the API ranks them.
 */
export function organizationEventTable(trends: OrganizationTrends): OrganizationEventTable {
  const showsTakings = trends.figures.takings != null;
  const rows = trends.events.map((event) => tableRow(event, showsTakings, trends.timezone));
  const takings = (row: OrganizationEventTableRow) => row.takingsCents ?? 0;
  rows.sort(
    (a, b) =>
      (showsTakings ? takings(b) - takings(a) : 0) ||
      b.ticketsSold - a.ticketsSold ||
      b.pageViews - a.pageViews ||
      compareText(a.name, b.name) ||
      compareText(a.id, b.id),
  );
  return { showsTakings, rows };
}

function tableRow(event: OrganizationTrendEvent, showsTakings: boolean, platformTimeZone: string): OrganizationEventTableRow {
  return {
    id: event.event_id,
    name: event.name,
    startsAt: event.starts_at,
    timeZone: event.timezone || platformTimeZone,
    ticketsSold: event.tickets_sold,
    takingsCents: showsTakings ? (event.takings_cents ?? 0) : null,
    pageViews: event.page_views,
    salesTrendsHref: eventSalesTrendsHref(event.event_id),
    reachHref: eventReachHref(event.event_id),
  };
}

/** Code-unit order; the API's byte order agrees on any name in the Basic Multilingual Plane. */
function compareText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}
