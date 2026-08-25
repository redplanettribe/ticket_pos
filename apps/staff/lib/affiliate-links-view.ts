// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import type { AffiliateLink } from "./affiliates-api.ts";

/**
 * The Affiliate Links table's view rules (#425, #427, #428): how the rows an
 * Event's tab has already loaded are ordered and narrowed on screen.
 *
 * Everything here is a pure transform of the array `listAffiliateLinks` returns
 * in full — the endpoint is unpaginated and orders by `created_at DESC, id DESC`
 * — so the component stays a renderer and the rules get `node --test` coverage.
 * Nothing is URL state: the default view is what every fresh visit should show,
 * and sort and search survive a rename, toggle or delete because they live
 * beside the list in component state.
 */

/** The columns a header click can sort on. Status is not one: it is a filter
 * in disguise, and none is being added. */
export type AffiliateSortField = "clicks" | "sales" | "net_proceeds" | "name" | "created";

export type SortDir = "asc" | "desc";

export type AffiliateSort = { field: AffiliateSortField; dir: SortDir };

/** The fields the sort reads. A subset of `AffiliateLink`, so a test builds
 * rows from what it is about rather than a full API object. */
export type AffiliateLinkRow = Pick<
  AffiliateLink,
  "id" | "name" | "clicks" | "sales_count" | "net_proceeds_cents" | "created_at"
>;

/** What the tab opens on: the links driving the most Clicks first. */
export const DEFAULT_AFFILIATE_SORT: AffiliateSort = { field: "clicks", dir: "desc" };

/** The direction a column starts in when selected: A→Z for names, largest or
 * newest first for everything else, so switching columns never lands on Z→A
 * or oldest-first. */
export function defaultDirFor(field: AffiliateSortField): SortDir {
  return field === "name" ? "asc" : "desc";
}

/** The sort after a header click: the active header flips, any other header
 * is selected in its natural direction. */
export function nextAffiliateSort(current: AffiliateSort, clicked: AffiliateSortField): AffiliateSort {
  if (current.field === clicked) {
    return { field: clicked, dir: current.dir === "asc" ? "desc" : "asc" };
  }
  return { field: clicked, dir: defaultDirFor(clicked) };
}

/**
 * Whether this Event's links are measured in sales at all, read off the rows
 * themselves: the API suppresses both attribution figures on an Event that
 * registers externally, and that absence is the only signal the table needs.
 * With no links yet there is nothing to explain either way, and the ordinary
 * wording (and column set) stands.
 */
export function isAttributionMeasured(links: readonly AffiliateLinkRow[]): boolean {
  return links.length === 0 || links.some((link) => link.sales_count !== null);
}

/** Whether a field can be sorted on given what the Event measures. */
export function isSortAvailable(field: AffiliateSortField, measured: boolean): boolean {
  return measured || (field !== "sales" && field !== "net_proceeds");
}

/**
 * The sort actually applied. On an unmeasured Event the Sales and Net proceeds
 * columns are absent, so a sort on either — possible when the list turned out
 * unmeasured after the choice was made — falls back to the default rather than
 * ordering by a column nobody can see.
 */
export function resolveAffiliateSort(sort: AffiliateSort, measured: boolean): AffiliateSort {
  return isSortAvailable(sort.field, measured) ? sort : DEFAULT_AFFILIATE_SORT;
}

// Case-insensitive and accent-folding at the primary level, so "maría" and
// "María" tie (and fall to the tiebreak) and "Álvaro" files under A. No locale
// is pinned: the browser's own collation is what the reader expects, and every
// locale this app ships agrees on Latin-script base ordering.
const nameCollator = new Intl.Collator(undefined, { sensitivity: "base" });

// Numbers with a hole: a null figure (an Event that registers externally) is
// unavailable, not zero, and sorts after every real figure whichever way the
// column points. Two nulls are a tie.
function compareNullableNumber(a: number | null, b: number | null, dir: SortDir): number {
  if (a === null || b === null) {
    if (a === null && b === null) {
      return 0;
    }
    return a === null ? 1 : -1;
  }
  return dir === "asc" ? a - b : b - a;
}

function compareByField(a: AffiliateLinkRow, b: AffiliateLinkRow, sort: AffiliateSort): number {
  switch (sort.field) {
    case "clicks":
      return sort.dir === "asc" ? a.clicks - b.clicks : b.clicks - a.clicks;
    case "sales":
      return compareNullableNumber(a.sales_count, b.sales_count, sort.dir);
    case "net_proceeds":
      return compareNullableNumber(a.net_proceeds_cents, b.net_proceeds_cents, sort.dir);
    case "name": {
      const byName = nameCollator.compare(a.name, b.name);
      return sort.dir === "asc" ? byName : -byName;
    }
    case "created": {
      const byCreated = compareISO(a.created_at, b.created_at);
      return sort.dir === "asc" ? byCreated : -byCreated;
    }
  }
}

// ISO-8601 UTC timestamps from the API order as text; no Date is built.
function compareISO(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

// Ties break newest-first, then by id descending — the server's own order
// (`created_at DESC, id DESC`) inside every tie band, whichever column is
// active and whichever way it points, so equal rows never shuffle between loads.
function compareTiebreak(a: AffiliateLinkRow, b: AffiliateLinkRow): number {
  const byCreated = compareISO(b.created_at, a.created_at);
  if (byCreated !== 0) {
    return byCreated;
  }
  return a.id < b.id ? 1 : a.id > b.id ? -1 : 0;
}

/** The links in table order. Returns a new array; the input is left as it came. */
export function sortAffiliateLinks<T extends AffiliateLinkRow>(
  links: readonly T[],
  field: AffiliateSortField,
  dir: SortDir,
): T[] {
  const sort: AffiliateSort = { field, dir };
  return [...links].sort((a, b) => compareByField(a, b, sort) || compareTiebreak(a, b));
}

/**
 * The links a search query keeps: those whose name or code contains the
 * trimmed query, case-insensitively. Never the URL — its Event-slug part is
 * identical on every row, so a search for the Event's name would match all of
 * them. An empty (or all-whitespace) query keeps everything, in the order it
 * came; the filter composes with the sort either side, so order is preserved.
 */
export function filterAffiliateLinks<T extends { name: string; code: string }>(
  links: readonly T[],
  query: string,
): T[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return [...links];
  }
  return links.filter(
    (link) => link.name.toLowerCase().includes(needle) || link.code.toLowerCase().includes(needle),
  );
}
