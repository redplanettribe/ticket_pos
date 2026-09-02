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
 * OperatorInvoiceFilters mirrors the list endpoint's filter query params. Every
 * field is carried in the URL, so the whole view is a link.
 *
 * "all", `false` and "" are the unfiltered values and are never written to the
 * URL, which keeps the common view's address clean. The following slices of
 * spec #593 add `issuedFrom`, `issuedTo` and `environment` here, one field per
 * filter, alongside the sort in its own pair of params (#596-#598); nothing in
 * this module is shaped so that adding one costs more than a line.
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
};

/** The whole list, unnarrowed: what a bare /operator/invoicing shows. */
export const EMPTY_OPERATOR_INVOICE_FILTERS: OperatorInvoiceFilters = {
  kind: "all",
  status: "all",
  recipientWarningOnly: false,
  q: "",
};

/** The raw query params the list page reads, exactly as Next hands them over. */
export type OperatorInvoiceListSearchParams = {
  page?: string;
  kind?: string;
  status?: string;
  recipient_warning?: string;
  q?: string;
};

/** The list's whole URL state: which page of which narrowing. */
export type OperatorInvoiceListView = {
  page: number;
  filters: OperatorInvoiceFilters;
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
  };
}

/**
 * parseOperatorInvoiceListParams reads the list's whole state off the address
 * bar. The page component calls it server-side and hands the result to the
 * client as its initial state, so no filter is ever seeded from component
 * state alone.
 */
export function parseOperatorInvoiceListParams(
  searchParams: OperatorInvoiceListSearchParams,
): OperatorInvoiceListView {
  return {
    page: parseOperatorInvoicePage(searchParams.page),
    filters: parseOperatorInvoiceFilters(searchParams),
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
}

/**
 * operatorInvoiceListQuery builds the canonical query string for the list's
 * URL: the page (omitted when it is the first) plus every active filter. The
 * parser above round-trips it.
 */
export function operatorInvoiceListQuery(page: number, filters: OperatorInvoiceFilters): string {
  const params = new URLSearchParams();
  if (page > FIRST_PAGE) params.set("page", String(page));
  appendOperatorInvoiceFilters(params, filters);
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
): string {
  return operatorInvoiceListQuery(FIRST_PAGE, { ...filters, ...patch });
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
    filters.q !== ""
  );
}

/**
 * fetchOperatorInvoiceList asks for one page of Tax Invoices under the given
 * narrowing, through the BFF proxy, which forwards the query string verbatim.
 * The API does the narrowing, so the envelope's count is the narrowed count.
 *
 * Page and page size are always sent (the URL omits them at their defaults);
 * fifty per page is the default and is not a choice the UI offers.
 */
export async function fetchOperatorInvoiceList(
  page: number,
  filters: OperatorInvoiceFilters,
): Promise<OperatorInvoiceListPage> {
  const params = new URLSearchParams({
    page: String(Math.max(FIRST_PAGE, page)),
    page_size: String(OPERATOR_INVOICES_PAGE_SIZE),
  });
  appendOperatorInvoiceFilters(params, filters);
  return fetchEventsJSON<OperatorInvoiceListPage>(`${OPERATOR_INVOICES_PATH}?${params.toString()}`);
}
