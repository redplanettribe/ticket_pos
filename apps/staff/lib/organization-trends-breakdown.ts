// The breakdown of Organization Trends (#864, spec #858, ADR 0081): the
// Events one bucket of one figure was made of, opened by choosing that bucket
// on the figure's chart, as on Platform Trends.
//
// The backend answers everything: which Events, in what order, the total
// they add up to and whether the bucket is today. What is here is the request
// a chosen bucket names and the rows the panel draws. Nothing is ranked or
// added up here.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { fetchEventsJSON } from "./events-api.ts";
import type { OrganizationTrendGranularity, OrganizationTrends } from "./organization-trends.ts";

/** A figure of Organization Trends, by its name in both reads. */
export type OrganizationTrendFigureId = keyof OrganizationTrends["figures"];

// ---------------------------------------------------------------------------
// The response, field for field as
// GET /api/v1/staff/organization/trends/breakdown states it.
// ---------------------------------------------------------------------------

export type OrganizationTrendBreakdownEvent = {
  event_id: string;
  name: string;
  /** A count, or for Takings an amount in the Organization's minor units. */
  value: number;
};

export type OrganizationTrendBreakdown = {
  timezone: string;
  figure: OrganizationTrendFigureId;
  granularity: OrganizationTrendGranularity;
  first_day: string;
  last_day: string;
  /** True when the bucket is today, a day still running. */
  partial: boolean;
  /** What the reading states for the bucket; the Events add up to it. */
  total: number;
  /** Largest first; for Events held each counts one, earliest start first. */
  events: OrganizationTrendBreakdownEvent[];
};

// ---------------------------------------------------------------------------
// The selection and its request
// ---------------------------------------------------------------------------

/**
 * The bucket whose breakdown is open: the figure whose chart it was chosen
 * on, the bucket's key on that chart, and the days it covers. One at a time
 * for the whole section, so choosing a bucket on another chart moves the
 * panel there.
 */
export type OrganizationTrendBucketSelection = {
  figure: OrganizationTrendFigureId;
  key: string;
  firstDay: string;
  lastDay: string;
};

/** The staff app's own route for the read, which forwards to the Go API. */
export const ORGANIZATION_TREND_BREAKDOWN_API_PATH = "/api/settings/organization/trends/breakdown";

/**
 * The request a chosen bucket names: its days, as the API takes them. A day
 * (today included, which is a day whatever the span is drawn in) is asked for
 * by its first day alone; a month bucket by its first and last day, so a
 * month the span cuts is read as the days it has.
 */
export function organizationBreakdownRequestPath(selection: OrganizationTrendBucketSelection): string {
  const params = new URLSearchParams({ figure: selection.figure, first_day: selection.firstDay });
  if (selection.lastDay !== selection.firstDay) {
    params.set("last_day", selection.lastDay);
  }
  return `${ORGANIZATION_TREND_BREAKDOWN_API_PATH}?${params.toString()}`;
}

/** fetchOrganizationTrendBreakdown reads one breakdown through the BFF, by
 * the path `organizationBreakdownRequestPath` built. */
export async function fetchOrganizationTrendBreakdown(requestPath: string): Promise<OrganizationTrendBreakdown> {
  return fetchEventsJSON<OrganizationTrendBreakdown>(requestPath);
}

// ---------------------------------------------------------------------------
// The response to what is drawn
// ---------------------------------------------------------------------------

/** One Event of a breakdown, as its row is drawn. */
export type OrganizationBreakdownRow = {
  id: string;
  name: string;
  /** The Event's own page. */
  href: string;
  /** The figure beside the row, or null for Events held, where each Event
   * counts one like every other. */
  value: number | null;
};

export type OrganizationBreakdownView = {
  /** Today so far. */
  partial: boolean;
  /** Ranked (numbered) for every figure but Events held, which is a list. */
  ranked: boolean;
  total: number;
  rows: OrganizationBreakdownRow[];
};

/** The page of one Event, which each row links to. */
export function eventPath(eventId: string): string {
  return `/events/${encodeURIComponent(eventId)}`;
}

/** organizationBreakdownView maps a breakdown to what its panel draws, in
 * the order the backend ranked the Events. */
export function organizationBreakdownView(breakdown: OrganizationTrendBreakdown): OrganizationBreakdownView {
  const ranked = breakdown.figure !== "events_held";
  return {
    partial: breakdown.partial,
    ranked,
    total: breakdown.total,
    rows: breakdown.events.map((event) => ({
      id: event.event_id,
      name: event.name,
      href: eventPath(event.event_id),
      value: ranked ? event.value : null,
    })),
  };
}
