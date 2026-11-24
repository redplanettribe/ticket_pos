// The breakdown of Platform Trends (#822, spec #808, ADR 0080): what one
// bucket of one figure was made of, opened by selecting that bucket on the
// figure's chart.
//
// The backend answers everything: who contributed, in what order, the totals
// they add up to, whether the bucket is partial, sales only or not recorded.
// What is here is the little the page does with that answer: the request a
// selected bucket names, and the shape the panel draws for each figure. No
// figure is derived, ranked or added up here.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { fetchEventsJSON } from "./events-api.ts";
import type { PlatformTrendGranularity, PlatformTrends } from "./platform-trends.ts";

/** A figure of Platform Trends, by its name in both reads. */
export type PlatformTrendFigureId = keyof PlatformTrends["figures"];

// ---------------------------------------------------------------------------
// The response, field for field as
// GET /api/v1/operator/platform-trends/breakdown states it.
// ---------------------------------------------------------------------------

/** What made an Organization active in a bucket. */
export type PlatformTrendActivityCause = "sale" | "member" | "both";

export type PlatformTrendBreakdownTotal = {
  /** "all" for a count, the currency code for a money figure. */
  key: string;
  /** A count, or an amount in the minor units of the currency. */
  value: number;
};

export type PlatformTrendBreakdownOrganization = {
  organization_id: string;
  name: string;
  /** The total this part counts toward. */
  key: string;
  value: number;
  /** Stated for Active Organizations, null for every other figure. */
  cause: PlatformTrendActivityCause | null;
};

export type PlatformTrendBreakdownEvent = {
  event_id: string;
  name: string;
  /** The Event's Organization, whose operator page an Event links to. */
  organization_id: string;
  organization_name: string;
  key: string;
  value: number;
};

/** The Active Customers of a bucket counted by kind: counts, and nobody. */
export type PlatformTrendBreakdownCustomerKinds = {
  new: number;
  returning: number;
  /** A part of `returning`, not a third kind beside it. */
  returning_bought: number;
};

export type PlatformTrendBreakdown = {
  timezone: string;
  figure: PlatformTrendFigureId;
  granularity: PlatformTrendGranularity;
  first_day: string;
  last_day: string;
  /** True when the bucket is today, a day still running. */
  partial: boolean;
  /** False when the bucket has no figure to break down, which is not a zero. */
  recorded: boolean;
  /** True when, for a day of the bucket, only the Organizations a Ticket Sale
   * was recorded for are listed. */
  sale_half_only: boolean;
  totals: PlatformTrendBreakdownTotal[];
  organizations: PlatformTrendBreakdownOrganization[];
  events: PlatformTrendBreakdownEvent[];
  customer_kinds: PlatformTrendBreakdownCustomerKinds | null;
};

// ---------------------------------------------------------------------------
// The selection and its request
// ---------------------------------------------------------------------------

/**
 * The bucket whose breakdown is open: which figure's chart it was chosen on,
 * the bucket's key on that chart, and the days it covers. One at a time for
 * the whole section, so choosing a bucket on another chart moves the panel
 * there.
 */
export type TrendBucketSelection = {
  figure: PlatformTrendFigureId;
  key: string;
  firstDay: string;
  lastDay: string;
  /** True for today, which is always asked for as a day. */
  partial: boolean;
};

/** The staff app's own route for the read, which forwards to the Go API. */
export const PLATFORM_TREND_BREAKDOWN_API_PATH = "/api/operator/platform-trends/breakdown";

/**
 * The request a selected bucket names. `granularity` is the reading's, as the
 * first read states it. Today is asked for as a day whatever the span is drawn
 * in, as the API has it. A week is asked for with its last day, so the stub a
 * custom range may begin with is read as the days it has and not as seven.
 */
export function breakdownRequestPath(selection: TrendBucketSelection, granularity: PlatformTrendGranularity): string {
  const params = new URLSearchParams({ figure: selection.figure, first_day: selection.firstDay });
  if (granularity === "week" && !selection.partial) {
    params.set("granularity", "week");
    params.set("last_day", selection.lastDay);
  } else {
    params.set("granularity", "day");
  }
  return `${PLATFORM_TREND_BREAKDOWN_API_PATH}?${params.toString()}`;
}

/** fetchPlatformTrendBreakdown reads one breakdown through the BFF, by the
 * path `breakdownRequestPath` built. Platform Operator only. */
export async function fetchPlatformTrendBreakdown(requestPath: string): Promise<PlatformTrendBreakdown> {
  return fetchEventsJSON<PlatformTrendBreakdown>(requestPath);
}

// ---------------------------------------------------------------------------
// The response to what is drawn
// ---------------------------------------------------------------------------

/** The operator page of an Organization, which every Organization in a
 * breakdown links to, and every Event through its Organization: there is no
 * operator page for an Event. */
export function operatorOrganizationPath(organizationId: string): string {
  return `/operator/organizations/${encodeURIComponent(organizationId)}`;
}

/** One Organization or Event of a breakdown, as its row is drawn. */
export type BreakdownRow = {
  id: string;
  name: string;
  /** The Organization's operator page: its own, or for an Event its
   * Organization's. */
  href: string;
  /** For an Event, the Organization it belongs to. Null for an Organization. */
  organizationName: string | null;
  /** The figure beside the row, in the body's unit, or null where the row is
   * an entry of a list and counts one like every other. */
  value: number | null;
  cause: PlatformTrendActivityCause | null;
};

/** The contributors toward one total: all of them for a count, those of one
 * currency for money. */
export type BreakdownGroup = {
  key: string;
  /** The currency code of a money group, which heads it. Null for a count. */
  currency: string | null;
  total: number;
  organizations: BreakdownRow[];
  events: BreakdownRow[];
};

/**
 * What a row's figure is: a count, an amount in its group's currency, the
 * days of the bucket an Organization was active on, or nothing, for a list.
 */
export type BreakdownUnit = "count" | "money" | "days" | "none";

export type BreakdownBody =
  /** No figure for the bucket, which is not a zero. */
  | { kind: "notRecorded" }
  /** A count alone: new Verified Customers, which names nobody. */
  | { kind: "count"; total: number }
  /** Active Customers by kind: counts, and nobody. */
  | { kind: "customerKinds"; total: number | null; new: number; returning: number; returningBought: number }
  /** Recorded, and nobody contributed. */
  | { kind: "empty" }
  | { kind: "contributors"; unit: BreakdownUnit; groups: BreakdownGroup[] };

/** One breakdown as the panel draws it. Tokens and data: the panel owns the
 * words. */
export type BreakdownView = {
  /** Today so far. */
  partial: boolean;
  /** The Organizations listed are, for a day of the bucket, those with a
   * recorded Ticket Sale and no others. */
  salesOnly: boolean;
  /** An activity figure over more than a day: its counts add up the days, so
   * a subject active on two days counts twice. */
  sumOfDays: boolean;
  body: BreakdownBody;
};

const MONEY_FIGURES: readonly PlatformTrendFigureId[] = ["money_sold", "platform_fee_earned"];
const ACTIVITY_FIGURES: readonly PlatformTrendFigureId[] = ["active_organizations", "active_customers"];
/** The figures whose breakdown is a list, each entry counting one. */
const LIST_FIGURES: readonly PlatformTrendFigureId[] = ["events_published", "new_organizations"];
/** The figures that count Customers. Nothing of theirs is ever drawn as a
 * list, whatever a response carries. */
const CUSTOMER_FIGURES: readonly PlatformTrendFigureId[] = ["active_customers", "new_verified_customers"];

function unitOf(breakdown: PlatformTrendBreakdown): BreakdownUnit {
  if (MONEY_FIGURES.includes(breakdown.figure)) return "money";
  if (LIST_FIGURES.includes(breakdown.figure)) return "none";
  if (breakdown.figure === "active_organizations") {
    // Over a day every Organization listed was active on that one day.
    return breakdown.first_day === breakdown.last_day ? "none" : "days";
  }
  return "count";
}

function bodyOf(breakdown: PlatformTrendBreakdown): BreakdownBody {
  if (!breakdown.recorded) {
    return { kind: "notRecorded" };
  }
  const totalOf = (key: string) => breakdown.totals.find((total) => total.key === key)?.value ?? null;
  if (CUSTOMER_FIGURES.includes(breakdown.figure)) {
    const kinds = breakdown.customer_kinds;
    return kinds === null
      ? { kind: "count", total: totalOf("all") ?? 0 }
      : {
          kind: "customerKinds",
          total: totalOf("all"),
          new: kinds.new,
          returning: kinds.returning,
          returningBought: kinds.returning_bought,
        };
  }
  const unit = unitOf(breakdown);
  const value = (stated: number) => (unit === "none" ? null : stated);
  const money = unit === "money";
  // The backend states one total per key and ranks the contributors within
  // it, so a group is the contributors of a key in the order they came. A key
  // nobody contributed to (a currency nothing was sold in) is left out.
  const groups = breakdown.totals
    .map((total): BreakdownGroup => ({
      key: total.key,
      currency: money ? total.key : null,
      total: total.value,
      organizations: breakdown.organizations
        .filter((organization) => organization.key === total.key)
        .map((organization) => ({
          id: organization.organization_id,
          name: organization.name,
          href: operatorOrganizationPath(organization.organization_id),
          organizationName: null,
          value: value(organization.value),
          cause: organization.cause,
        })),
      events: breakdown.events
        .filter((event) => event.key === total.key)
        .map((event) => ({
          id: event.event_id,
          name: event.name,
          href: operatorOrganizationPath(event.organization_id),
          organizationName: event.organization_name,
          value: value(event.value),
          cause: null,
        })),
    }))
    .filter((group) => group.organizations.length > 0 || group.events.length > 0);
  return groups.length === 0 ? { kind: "empty" } : { kind: "contributors", unit, groups };
}

/**
 * breakdownView maps a breakdown to what its panel draws, each figure in its
 * own shape: ranked Organizations and Events for Tickets Sold and money (one
 * group per currency), ranked Events for Page Views, the list for Events
 * Published and new Organizations, the list with each cause for Active
 * Organizations, counts by kind for Active Customers, and a count for new
 * Verified Customers.
 */
export function breakdownView(breakdown: PlatformTrendBreakdown): BreakdownView {
  return {
    partial: breakdown.partial,
    salesOnly: breakdown.sale_half_only,
    sumOfDays: ACTIVITY_FIGURES.includes(breakdown.figure) && breakdown.first_day !== breakdown.last_day,
    body: bodyOf(breakdown),
  };
}
