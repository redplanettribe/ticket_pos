// The Tax Invoices list's URL state (#594, spec #593): the filters record, the
// parser that reads it off the address bar, the builder that writes it back,
// and the fetch that asks the API for exactly the view the URL names.
//
// Written after lib/sales-api.ts, deliberately: the Sales list already decided
// that a list's narrowing lives in the URL rather than in component state, so
// a view is shareable, survives a reload, and walks the back button. One
// builder drives both the address bar and the request, which is what keeps
// them in lockstep — a filter that reached the fetch but not the URL would
// show a narrowing nobody could bookmark.
//
// Relative imports carry the ".ts" extension (as sales-api.ts does) so the
// module graph resolves under `node --test` as well as the bundler — the unit
// tests beside this file import it directly.
import { fetchEventsJSON } from "./events-api.ts";
import {
  INVOICE_STATUSES,
  OPERATOR_INVOICES_PAGE_SIZE,
  OPERATOR_INVOICES_PATH,
  type InvoiceKindFilter,
  type InvoiceStatusFilter,
  type OperatorInvoiceListPage,
} from "./operator-api.ts";

/** The kind filter's options, in the order the select offers them (#477). */
export const INVOICE_KIND_FILTERS = [
  "all",
  "manual",
  "sale",
  "credit_note",
] as const satisfies readonly InvoiceKindFilter[];

/**
 * The status filter's options: every status a document can be in, in the
 * order a document travels, behind "all" (#578, ADR 0068). Built from the
 * vocabulary itself so a status added to the API is offered here too.
 */
export const INVOICE_STATUS_FILTERS = [
  "all",
  ...INVOICE_STATUSES,
] as const satisfies readonly InvoiceStatusFilter[];

/** The first page. Named because the page-reset rule below is about this one. */
export const FIRST_PAGE = 1;

/**
 * The columns the Tax Invoices list may be read in order of (#597), and the
 * two directions — mirroring the API's allowlist, which refuses anything else
 * outright.
 *
 * FOUR AND NO MORE. Kind, Sale, Status and Country are not here on purpose:
 * each of them is answered better by a filter than by an order, since an
 * operator wants to see one kind or one status alone rather than to read a
 * run of them.
 *
 * A SORT IS NOT A FILTER, which is why it lives here rather than on the
 * filters record. It narrows nothing, so an empty page under a sort must
 * still read "the platform has issued no documents" rather than "nothing
 * matches these filters" — and the reset the next slice adds (#598) has to
 * clear it separately for the same reason.
 */
export const OPERATOR_INVOICE_SORTS = ["date", "number", "total", "recipient"] as const;
export type OperatorInvoiceSort = (typeof OPERATOR_INVOICE_SORTS)[number];

export const OPERATOR_INVOICE_DIRS = ["asc", "desc"] as const;
export type OperatorInvoiceDir = (typeof OPERATOR_INVOICE_DIRS)[number];

/**
 * The order a bare /operator/invoicing shows: newest Emission Date first,
 * which is the order this list has always had. It is never written to the
 * URL, so the common view's address stays clean and every link that predates
 * sorting still names the view it always named.
 */
export const DEFAULT_OPERATOR_INVOICE_SORT: OperatorInvoiceSort = "date";
export const DEFAULT_OPERATOR_INVOICE_DIR: OperatorInvoiceDir = "desc";

/**
 * The direction a column starts in when it is first clicked: newest and
 * largest first for the date and the total, A→Z for the Recipient, and
 * ASCENDING for the number — a run of numbers is read upward when the
 * question is where the gap is.
 */
export function defaultDirForOperatorInvoiceSort(sort: OperatorInvoiceSort): OperatorInvoiceDir {
  return sort === "number" || sort === "recipient" ? "asc" : "desc";
}

/**
 * OperatorInvoiceFilters mirrors the list endpoint's filter query params. Every
 * field is carried in the URL, so the whole view is a link.
 *
 * "all", `false` and "" are the unfiltered values and are never written to the
 * URL, which keeps the common view's address clean. The last slice of spec
 * #593 adds `environment` here, one field per filter (#598); the sort keeps
 * its own pair of params outside this record (#597), since it narrows
 * nothing. Nothing in this module is shaped so that adding a filter costs
 * more than a line.
 */
export type OperatorInvoiceFilters = {
  kind: InvoiceKindFilter;
  status: InvoiceStatusFilter;
  /** Only the documents the SRI warned about the Recipient of (#482, ADR 0061). */
  recipientWarningOnly: boolean;
  /**
   * The search term (#595): one case-insensitive substring the API matches
   * against the printed number, the Recipient's legal name, the Recipient's
   * Tax ID and the Sale Confirmation reference. "" is no search.
   *
   * It is one string and not four because the operator does not know which of
   * the four they are holding — a buyer reads out a name, an accountant a
   * number, a receipt a reference. The API owns what matching means; this
   * side only carries the term.
   */
  q: string;
  /**
   * The Emission Date range (#596): inclusive calendar-day bounds, each
   * "YYYY-MM-DD" or "" for an open bound, so "everything emitted in August"
   * and "everything since July 1st" are both one link.
   *
   * They are the EMISSION DATE's and nothing else's — the day in the
   * Issuer's country the document carries, which is what the Date column
   * shows — so a document not yet signed falls out of a bounded view. That
   * rule lives in the API, which also refuses a malformed or inverted range;
   * nothing here second-guesses either, since a date input can produce
   * neither and a hand-edited address bar deserves the API's answer rather
   * than a silently widened view.
   */
  issuedFrom: string;
  issuedTo: string;
};

/** The whole list, unnarrowed: what a bare /operator/invoicing shows. */
export const EMPTY_OPERATOR_INVOICE_FILTERS: OperatorInvoiceFilters = {
  kind: "all",
  status: "all",
  recipientWarningOnly: false,
  q: "",
  issuedFrom: "",
  issuedTo: "",
};

/** The raw query params the list page reads, exactly as Next hands them over. */
export type OperatorInvoiceListSearchParams = {
  page?: string;
  kind?: string;
  status?: string;
  recipient_warning?: string;
  q?: string;
  issued_from?: string;
  issued_to?: string;
  sort?: string;
  dir?: string;
};

/**
 * The list's whole URL state: which page of which narrowing, read in which
 * order. The sort sits BESIDE the filters rather than inside them, as the
 * Sales list keeps it, because it narrows nothing.
 */
export type OperatorInvoiceListView = {
  page: number;
  filters: OperatorInvoiceFilters;
  sort: OperatorInvoiceSort;
  dir: OperatorInvoiceDir;
};

// parseOperatorInvoicePage reads the page number, flooring at 1 — a page the
// URL cannot express (absent, zero, negative, "two") is page one rather than
// an error, since a mistyped address should still show the operator a list.
function parseOperatorInvoicePage(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "", 10);
  return Number.isFinite(parsed) && parsed >= FIRST_PAGE ? parsed : FIRST_PAGE;
}

// parseOperatorInvoiceFilters narrows each raw value to its allowlist, falling
// back to the unfiltered value. A kind or status this app does not know widens
// the view rather than narrowing it to nothing the operator can explain — the
// API re-validates whatever does get sent.
function parseOperatorInvoiceFilters(
  searchParams: OperatorInvoiceListSearchParams,
): OperatorInvoiceFilters {
  const kind = searchParams.kind as InvoiceKindFilter | undefined;
  const status = searchParams.status as InvoiceStatusFilter | undefined;
  return {
    kind: kind && INVOICE_KIND_FILTERS.includes(kind) ? kind : "all",
    status: status && INVOICE_STATUS_FILTERS.includes(status) ? status : "all",
    // Only the literal "true" narrows: the Operator Dashboard's deep link
    // spells it that way (#482) and it is the value this builder writes.
    recipientWarningOnly: searchParams.recipient_warning === "true",
    // Trimmed here as well as in the box, because a link can carry whitespace
    // the box never typed; a term that trims to nothing is no search, so
    // `?q=%20` shows the whole list rather than an empty one (#595).
    q: (searchParams.q ?? "").trim(),
    // Carried verbatim (bar the trim a link can pick up): what a calendar day
    // means is the API's, and a bound this app rewrote would show a view the
    // address bar does not name. A malformed one is the API's 400, not a
    // filter quietly dropped here (#596).
    issuedFrom: (searchParams.issued_from ?? "").trim(),
    issuedTo: (searchParams.issued_to ?? "").trim(),
  };
}

// parseOperatorInvoiceSort and parseOperatorInvoiceDir narrow a raw URL value
// to the allowlist, falling back to the default order. A key this app does
// not know shows the list in the order it has always had rather than in no
// order at all — and, since the default is never written back to the URL, a
// hand-edited `?sort=colour` is dropped on the next click rather than carried
// to an API that would refuse it.
function parseOperatorInvoiceSort(raw: string | undefined): OperatorInvoiceSort {
  return OPERATOR_INVOICE_SORTS.includes(raw as OperatorInvoiceSort)
    ? (raw as OperatorInvoiceSort)
    : DEFAULT_OPERATOR_INVOICE_SORT;
}

function parseOperatorInvoiceDir(raw: string | undefined): OperatorInvoiceDir {
  return OPERATOR_INVOICE_DIRS.includes(raw as OperatorInvoiceDir)
    ? (raw as OperatorInvoiceDir)
    : DEFAULT_OPERATOR_INVOICE_DIR;
}

/**
 * parseOperatorInvoiceListParams reads the list's whole state off the address
 * bar. The page component calls it server-side and hands the result to the
 * client as its initial state, so no filter and no ordering is ever seeded
 * from component state alone.
 */
export function parseOperatorInvoiceListParams(
  searchParams: OperatorInvoiceListSearchParams,
): OperatorInvoiceListView {
  return {
    page: parseOperatorInvoicePage(searchParams.page),
    filters: parseOperatorInvoiceFilters(searchParams),
    sort: parseOperatorInvoiceSort(searchParams.sort),
    dir: parseOperatorInvoiceDir(searchParams.dir),
  };
}

// appendOperatorInvoiceFilters writes the narrowing values onto a params
// object, omitting anything that is not narrowing anything.
function appendOperatorInvoiceFilters(
  params: URLSearchParams,
  filters: OperatorInvoiceFilters,
): void {
  if (filters.kind !== "all") params.set("kind", filters.kind);
  if (filters.status !== "all") params.set("status", filters.status);
  if (filters.recipientWarningOnly) params.set("recipient_warning", "true");
  if (filters.q !== "") params.set("q", filters.q);
  if (filters.issuedFrom !== "") params.set("issued_from", filters.issuedFrom);
  if (filters.issuedTo !== "") params.set("issued_to", filters.issuedTo);
}

// appendOperatorInvoiceSort writes the sort pair, omitting it entirely at the
// default order — so the whole-list view's address stays the bare path it was
// before this list could be sorted, and every link written before #597 still
// names the same view. The two params are written together or not at all: a
// `dir` with no `sort` names nothing.
function appendOperatorInvoiceSort(
  params: URLSearchParams,
  sort: OperatorInvoiceSort,
  dir: OperatorInvoiceDir,
): void {
  if (sort !== DEFAULT_OPERATOR_INVOICE_SORT || dir !== DEFAULT_OPERATOR_INVOICE_DIR) {
    params.set("sort", sort);
    params.set("dir", dir);
  }
}

/**
 * operatorInvoiceListQuery builds the canonical query string for the list's
 * URL: the page (omitted when it is the first) plus every active filter and a
 * non-default order. The parser above round-trips it.
 */
export function operatorInvoiceListQuery(
  page: number,
  filters: OperatorInvoiceFilters,
  sort: OperatorInvoiceSort = DEFAULT_OPERATOR_INVOICE_SORT,
  dir: OperatorInvoiceDir = DEFAULT_OPERATOR_INVOICE_DIR,
): string {
  const params = new URLSearchParams();
  if (page > FIRST_PAGE) params.set("page", String(page));
  appendOperatorInvoiceFilters(params, filters);
  appendOperatorInvoiceSort(params, sort, dir);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/**
 * operatorInvoiceListQueryAfterFilterChange merges a filter change into the
 * current filters and returns to the first page.
 *
 * The reset is the rule, not the caller's discretion: a narrower result has
 * fewer pages, so staying on page seven would land the operator on an empty
 * one. Paging itself keeps its page — that is the one change that does.
 */
export function operatorInvoiceListQueryAfterFilterChange(
  filters: OperatorInvoiceFilters,
  patch: Partial<OperatorInvoiceFilters>,
  sort: OperatorInvoiceSort = DEFAULT_OPERATOR_INVOICE_SORT,
  dir: OperatorInvoiceDir = DEFAULT_OPERATOR_INVOICE_DIR,
): string {
  return operatorInvoiceListQuery(FIRST_PAGE, { ...filters, ...patch }, sort, dir);
}

/**
 * operatorInvoiceListQueryAfterSortChange answers what one press on a column
 * header means: the active column flips direction, a new column starts in the
 * direction that column is naturally read in, and either way the list returns
 * to the first page.
 *
 * THE PAGE RESET IS THE SAME RULE A FILTER CHANGE FOLLOWS, for a different
 * reason: a reordered list has the same number of pages, but page seven of it
 * holds different documents, and an operator who reordered to find the
 * largest total wants it on the page they are looking at.
 *
 * The filters are untouched: an order is not a narrowing.
 */
export function operatorInvoiceListQueryAfterSortChange(
  filters: OperatorInvoiceFilters,
  sort: OperatorInvoiceSort,
  dir: OperatorInvoiceDir,
  pressed: OperatorInvoiceSort,
): string {
  const nextDir: OperatorInvoiceDir =
    pressed === sort
      ? dir === "asc"
        ? "desc"
        : "asc"
      : defaultDirForOperatorInvoiceSort(pressed);
  return operatorInvoiceListQuery(FIRST_PAGE, filters, pressed, nextDir);
}

/**
 * hasActiveOperatorInvoiceFilters reports whether the view is narrowed at all
 * — what tells "nothing matches these filters" apart from "no documents at
 * all" in the empty state.
 */
export function hasActiveOperatorInvoiceFilters(filters: OperatorInvoiceFilters): boolean {
  return (
    filters.kind !== "all" ||
    filters.status !== "all" ||
    filters.recipientWarningOnly ||
    // A search is a narrowing like any other (#595): a term that found nothing
    // must say so, not report that the platform has issued no documents.
    filters.q !== "" ||
    // A date range is a narrowing too (#596), and the one most likely to come
    // back empty: a month the platform emitted nothing in is not a platform
    // that has issued nothing.
    filters.issuedFrom !== "" ||
    filters.issuedTo !== ""
  );
}

/**
 * fetchOperatorInvoiceList asks for one page of Tax Invoices under the given
 * narrowing and in the given order, through the BFF proxy, which forwards the
 * query string verbatim. The API does the narrowing and the ordering, so the
 * envelope's count is the narrowed count and the rows arrive already sorted —
 * nothing here reorders a page, which would only ever sort the fifty
 * documents in hand rather than the result they were drawn from.
 *
 * Page and page size are always sent (the URL omits them at their defaults);
 * fifty per page is the default and is not a choice the UI offers.
 */
export async function fetchOperatorInvoiceList(
  page: number,
  filters: OperatorInvoiceFilters,
  sort: OperatorInvoiceSort = DEFAULT_OPERATOR_INVOICE_SORT,
  dir: OperatorInvoiceDir = DEFAULT_OPERATOR_INVOICE_DIR,
): Promise<OperatorInvoiceListPage> {
  const params = new URLSearchParams({
    page: String(Math.max(FIRST_PAGE, page)),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  appendOperatorInvoiceFilters(params, filters);
  appendOperatorInvoiceSort(params, sort, dir);
  return fetchEventsJSON<OperatorInvoiceListPage>(`${OPERATOR_INVOICES_PATH}?${params.toString()}`);
}
