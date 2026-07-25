// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { fetchEventsJSON } from "./events-api.ts";

// Types mirror the Go Sales list response (internal/sales). Field names match
// the JSON the API emits so rows render verbatim.

export type SaleTicketType = {
  ticket_type_name: string;
  quantity: number;
};

export type SaleListRow = {
  id: string;
  customer_first_name: string;
  customer_last_name: string;
  customer_email: string;
  ticket_types: SaleTicketType[];
  amount_cents: number;
  currency: string;
  sold_at: string;
  channel: string;
  source: string | null;
  status: string;
  confirmation_ref: string;
  // Row-detail expand: the recorded-at (created_at) time and payment method.
  recorded_at: string;
  payment_method: string | null;
};

export type SalesPagination = {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

// SalesListResponse is the ADR-0006 nested envelope carried inside the transport
// envelope's `data`: the page of rows plus its pagination metadata.
export type SalesListResponse = {
  data: SaleListRow[];
  pagination: SalesPagination;
};

export const SALES_PAGE_SIZE = 50;

// SaleSortField is an allowlisted Sales list sort column; SaleSortDir a direction.
// Both mirror the backend allowlists (ADR-0006) and are carried in the URL.
export type SaleSortField = "sold_at" | "recorded_at" | "customer" | "amount";
export type SaleSortDir = "asc" | "desc";

export const SALE_SORT_FIELDS: readonly SaleSortField[] = [
  "sold_at",
  "recorded_at",
  "customer",
  "amount",
];

export const DEFAULT_SALE_SORT: SaleSortField = "sold_at";
export const DEFAULT_SALE_DIR: SaleSortDir = "desc";

// parseSaleSort resolves a raw URL value to an allowlisted sort field, falling
// back to the default (sold_at) — mirrors the backend so the UI and API agree.
export function parseSaleSort(raw: string | undefined): SaleSortField {
  return SALE_SORT_FIELDS.includes(raw as SaleSortField) ? (raw as SaleSortField) : DEFAULT_SALE_SORT;
}

// parseSaleDir resolves a raw URL value to a direction, falling back to desc.
export function parseSaleDir(raw: string | undefined): SaleSortDir {
  return raw === "asc" || raw === "desc" ? raw : DEFAULT_SALE_DIR;
}

// SalesFilters mirrors the endpoint's filter query params. Each field is carried
// in the URL so the view is shareable and survives a refresh. status defaults to
// "active"; every other field is empty when that dimension is unfiltered.
export type SalesFilters = {
  status: string;
  ticketTypeId: string;
  soldFrom: string;
  soldTo: string;
  q: string;
  channel: string;
  source: string;
  paymentMethod: string;
};

export const DEFAULT_SALES_STATUS = "active";

export const EMPTY_SALES_FILTERS: SalesFilters = {
  status: DEFAULT_SALES_STATUS,
  ticketTypeId: "",
  soldFrom: "",
  soldTo: "",
  q: "",
  channel: "",
  source: "",
  paymentMethod: "",
};

// appendSalesFilters writes the non-default filter values onto a params object.
// status is emitted only when it differs from the default so the common view
// keeps a clean URL (the API defaults to active).
function appendSalesFilters(params: URLSearchParams, filters: SalesFilters): void {
  if (filters.status && filters.status !== DEFAULT_SALES_STATUS) {
    params.set("status", filters.status);
  }
  if (filters.ticketTypeId) params.set("ticket_type_id", filters.ticketTypeId);
  if (filters.soldFrom) params.set("sold_from", filters.soldFrom);
  if (filters.soldTo) params.set("sold_to", filters.soldTo);
  if (filters.q) params.set("q", filters.q);
  if (filters.channel) params.set("channel", filters.channel);
  if (filters.source) params.set("source", filters.source);
  if (filters.paymentMethod) params.set("payment_method", filters.paymentMethod);
}

// appendSalesSort writes the sort/dir params, omitting them when they match the
// default (sold_at desc) so the common view keeps a clean, shareable URL.
function appendSalesSort(params: URLSearchParams, sort: SaleSortField, dir: SaleSortDir): void {
  if (sort !== DEFAULT_SALE_SORT || dir !== DEFAULT_SALE_DIR) {
    params.set("sort", sort);
    params.set("dir", dir);
  }
}

// salesListQuery builds the canonical query string for the Sales list URL: the
// page (omitted when 1) plus any active filters and non-default sort/dir. Driving
// both the address bar and the fetch from one builder keeps them in lockstep.
export function salesListQuery(
  page: number,
  filters: SalesFilters,
  sort: SaleSortField = DEFAULT_SALE_SORT,
  dir: SaleSortDir = DEFAULT_SALE_DIR,
): string {
  const params = new URLSearchParams();
  if (page > 1) params.set("page", String(page));
  appendSalesFilters(params, filters);
  appendSalesSort(params, sort, dir);
  const query = params.toString();
  return query ? `?${query}` : "";
}

// hasActiveSalesFilters reports whether the view is narrowed beyond the default
// (active status, no other filters) — used to tailor the empty state.
export function hasActiveSalesFilters(filters: SalesFilters): boolean {
  return (
    filters.status !== DEFAULT_SALES_STATUS ||
    Boolean(
      filters.ticketTypeId ||
        filters.soldFrom ||
        filters.soldTo ||
        filters.q ||
        filters.channel ||
        filters.source ||
        filters.paymentMethod,
    )
  );
}

// fetchSalesList proxies the Sales list endpoint via the BFF for the given page,
// filters, and sort. Filters and non-default sort/dir are appended so the fetch
// matches the URL state the list writes.
export async function fetchSalesList(
  eventId: string,
  page: number,
  filters: SalesFilters,
  sort: SaleSortField = DEFAULT_SALE_SORT,
  dir: SaleSortDir = DEFAULT_SALE_DIR,
): Promise<SalesListResponse> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(SALES_PAGE_SIZE),
  });
  appendSalesFilters(params, filters);
  appendSalesSort(params, sort, dir);
  return fetchEventsJSON<SalesListResponse>(`/api/events/${eventId}/sales?${params.toString()}`);
}

// rollupTicketTypes renders a sale's Ticket Types as "2× GA, 1× VIP".
export function rollupTicketTypes(ticketTypes: SaleTicketType[]): string {
  if (ticketTypes.length === 0) {
    return "—";
  }
  return ticketTypes.map((t) => `${t.quantity}× ${t.ticket_type_name}`).join(", ");
}

const CHANNEL_LABELS: Record<string, string> = {
  online: "Online",
  in_person: "In person",
  import: "Import",
};

const SOURCE_LABELS: Record<string, string> = {
  direct: "Direct",
  external_platform: "External platform",
};

const PAYMENT_METHOD_LABELS: Record<string, string> = {
  cash: "Cash",
  transfer: "Transfer",
  payphone: "PayPhone",
};

// channelSourceLabel renders the channel and (when present) source as
// "Import · Direct".
export function channelSourceLabel(channel: string, source: string | null): string {
  const channelLabel = CHANNEL_LABELS[channel] ?? channel;
  if (!source) {
    return channelLabel;
  }
  const sourceLabel = SOURCE_LABELS[source] ?? source;
  return `${channelLabel} · ${sourceLabel}`;
}

// paymentMethodLabel renders a payment method for the row-detail expand.
export function paymentMethodLabel(paymentMethod: string | null): string {
  if (!paymentMethod) {
    return "—";
  }
  return PAYMENT_METHOD_LABELS[paymentMethod] ?? paymentMethod;
}

// formatSaleTimestamp renders an ISO timestamp in the Event timezone (falling
// back to the viewer's locale zone when the Event has none).
export function formatSaleTimestamp(iso: string, timezone: string | null): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
    ...(timezone ? { timeZone: timezone } : {}),
  }).format(date);
}
