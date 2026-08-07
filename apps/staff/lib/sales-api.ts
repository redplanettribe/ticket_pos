// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { ApiError, fetchEventsJSON } from "./events-api.ts";

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
  // The Tax ID the sale was transacted under — the snapshot the search matches,
  // shown so a match is confirmable at a glance. Null together on sales recorded
  // without one (legacy rows and imports that never collected it, ADR 0016).
  tax_id_type: string | null;
  tax_id_number: string | null;
  // The Sale Reversal's provenance: when the sale was reversed and which side
  // caused it ("customer" or "staff"). Both null on an active sale, and on a
  // sale reversed before either was recorded (#117, ADR 0018).
  reversed_at: string | null;
  reversed_by: string | null;
};

export type SalesPagination = {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

// SalesListResponse is the ADR-0006 nested envelope carried inside the transport
// envelope's `data`: the page of rows plus its pagination metadata.
//
// reversed_count is how many of the Event's Ticket Sales are reversed — the
// whole Event, independent of every filter on the request, including the status
// filter that chose these rows. It rides the list rather than the Net Proceeds
// strip because a Sale Reversal must be visible to every Member of the Event and
// the strip is Org-Admin/Event-Owner only (#122, ADR 0018).
export type SalesListResponse = {
  data: SaleListRow[];
  pagination: SalesPagination;
  reversed_count: number;
};

// EventSalesSummary is the Sales tab stat strip: what this Event's active
// Online Sales have left the Organization after platform costs (already net —
// the platform's cut is never a number on this surface, ADR 0014), and how many
// active Ticket Sales the Event has made across every channel.
export type EventSalesSummary = {
  net_proceeds_cents: number;
  currency: string;
  sales_count: number;
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

// salesExportPath is the BFF path for the Sales Export under the given filters
// and sort. It is built from the same helpers the list's own fetch uses, so the
// file that arrives is the screen that was showing. Pagination is deliberately
// absent: the file is the whole answer, not a page of it.
export function salesExportPath(
  eventId: string,
  filters: SalesFilters,
  sort: SaleSortField = DEFAULT_SALE_SORT,
  dir: SaleSortDir = DEFAULT_SALE_DIR,
): string {
  const params = new URLSearchParams();
  appendSalesFilters(params, filters);
  appendSalesSort(params, sort, dir);
  const query = params.toString();
  return `/api/events/${eventId}/sales/export${query ? `?${query}` : ""}`;
}

// exportFilenameFrom reads the download's filename out of a Content-Disposition
// header. The backend decides the name so it is decided in one place; this only
// reads it back, falling back to a plain name if the header is missing.
function exportFilenameFrom(disposition: string | null): string {
  const match = disposition?.match(/filename="?([^"]+)"?/);
  return match?.[1] ?? "sales-export.xlsx";
}

// downloadSalesExport fetches the Sales Export and saves it as a file.
//
// It fetches a blob rather than navigating to the URL, and that is required
// rather than cosmetic: the endpoint returns a file on success and a JSON error
// envelope on failure, so a plain link would navigate the browser to raw JSON
// whenever the export was refused and the error we rely on would be invisible.
// Fetching also lets the caller show a spinner while a large file is built.
/** The error half of the envelope an export refusal arrives in. */
type ExportErrorEnvelope = {
  error?: {
    message?: string;
    code?: string;
    details?: { fields?: { field?: string; message?: string }[] };
  };
} | null;

// salesExportErrorMessage is what the person is shown when an export is refused.
//
// It prefers the FIELD error's message over the envelope's own. That is not a
// cosmetic preference: the row-cap refusal is a VALIDATION_FAILED, whose
// top-level message is the generic "Request validation failed", while the
// sentence that actually helps — how many sales matched, how many may be
// downloaded at once, and to narrow the filters — is the field error underneath
// it. Reading only the top level would replace the one useful message in the
// feature with boilerplate, and the cap is only survivable because the person is
// told which lever to pull.
export function salesExportErrorMessage(envelope: ExportErrorEnvelope): string {
  const fieldMessage = envelope?.error?.details?.fields?.find((f) => f?.message)?.message;
  return fieldMessage ?? envelope?.error?.message ?? "Failed to export sales";
}

export async function downloadSalesExport(
  eventId: string,
  filters: SalesFilters,
  sort: SaleSortField = DEFAULT_SALE_SORT,
  dir: SaleSortDir = DEFAULT_SALE_DIR,
): Promise<void> {
  const response = await fetch(salesExportPath(eventId, filters, sort, dir));
  if (!response.ok) {
    const envelope = (await response.json().catch(() => null)) as ExportErrorEnvelope;
    throw new ApiError(salesExportErrorMessage(envelope), envelope?.error?.code);
  }

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = exportFilenameFrom(response.headers.get("Content-Disposition"));
    document.body.appendChild(link);
    link.click();
    link.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
}

// fetchSalesSummary proxies the Event's sales summary via the BFF. The endpoint
// is Org-Admin/Event-Owner only, so Event Staff never reach this call.
export async function fetchSalesSummary(eventId: string): Promise<EventSalesSummary> {
  return fetchEventsJSON<EventSalesSummary>(`/api/events/${eventId}/sales/summary`);
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

// The one place the Payment Method value set is spelled out for the staff app:
// the filter options and the row-detail labels both read from it.
export const PAYMENT_METHODS = [
  { value: "cash", label: "Cash" },
  { value: "transfer", label: "Transfer" },
  { value: "payphone", label: "PayPhone" },
] as const;

const PAYMENT_METHOD_LABELS: Record<string, string> = Object.fromEntries(
  PAYMENT_METHODS.map((method) => [method.value, method.label]),
);

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

// The Tax ID Type labels an organizer reads on the Sales list. Spanish for the
// identifier names, because that is what is printed on the documents the buyer
// hands over — the same labels the Storefront checkout shows the buyer
// (apps/storefront/lib/tax-id.ts), mirrored here because the two apps share no
// domain package.
const TAX_ID_TYPE_LABELS: Record<string, string> = {
  cedula: "Cédula",
  ruc: "RUC",
  passport: "Pasaporte",
};

// taxIdLabel renders a sale's Tax ID snapshot as "Cédula: 1712345678" — the
// type as a human label with the number, unmasked, so an organizer can confirm
// a search match and copy the number for a declaration. A sale recorded without
// one renders a plain "—": history is shown as it is, never backfilled.
export function taxIdLabel(taxIdType: string | null, taxIdNumber: string | null): string {
  if (!taxIdType || !taxIdNumber) {
    return "—";
  }
  return `${TAX_ID_TYPE_LABELS[taxIdType] ?? taxIdType}: ${taxIdNumber}`;
}

// paymentMethodLabel renders a payment method for the row-detail expand.
export function paymentMethodLabel(paymentMethod: string | null): string {
  if (!paymentMethod) {
    return "—";
  }
  return PAYMENT_METHOD_LABELS[paymentMethod] ?? paymentMethod;
}

// Who caused a Sale Reversal, as an organizer reads it: the buyer undoing their
// own Online Sale inside the Reversal Window, their own staff undoing a Sale
// Import, or the platform recording a refund it made off-platform at the
// organization's request. The two answers a reversed row has to give are when
// and which side — and nothing of the platform's own money memo, which is
// operator-facing only (#125).
const REVERSAL_ACTOR_LABELS: Record<string, string> = {
  customer: "Customer",
  staff: "Staff",
  operator: "The platform",
};

// reversalLabel renders a reversed sale's provenance as "Jul 7, 2026, 12:00 by
// Customer". A sale reversed before the platform recorded either half has none,
// and says so plainly rather than guessing a time — history is never backfilled.
export function reversalLabel(
  reversedAt: string | null,
  reversedBy: string | null,
  timezone: string | null,
): string {
  if (!reversedAt) {
    return "Not recorded";
  }
  const when = formatSaleTimestamp(reversedAt, timezone);
  if (!reversedBy) {
    return when;
  }
  return `${when} by ${REVERSAL_ACTOR_LABELS[reversedBy] ?? reversedBy}`;
}

// reversedCountLabel states how many of the Event's Ticket Sales are reversed.
// An Event that has never had one says so plainly rather than showing a bare
// "0": the sentence is the whole point of the figure — it explains a total that
// dropped without anybody on staff touching it — and on a quiet Event the
// answer "nothing was reversed" is worth the same words.
export function reversedCountLabel(count: number): string {
  if (count <= 0) {
    return "No reversed sales";
  }
  return `${count} reversed sale${count === 1 ? "" : "s"}`;
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
