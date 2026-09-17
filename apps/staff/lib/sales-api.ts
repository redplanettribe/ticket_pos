// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { ApiError, fetchEventsJSON } from "./events-api.ts";
import type { ImportPreviewResult } from "./imports-api.ts";

// Types mirror the Go Sales list response (internal/sales). Field names match
// the JSON the API emits so rows render verbatim.

export type SaleTicketType = {
  // The id is here for the Correct form (#351), pre-filled from the row.
  ticket_type_id: string;
  ticket_type_name: string;
  quantity: number;
};

export type SaleListRow = {
  id: string;
  // The buyer's Customer id: what the Customer Dossier is addressed by (#638),
  // so a link to it never carries the email.
  customer_id: string;
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
  // The Sale Correction linkage (#350, ADR 0050): on a reversed sale, the
  // replacement that corrected it; on the replacement, the sale it stands in
  // for. Both null until a correction is recorded — a sale reversed on its own
  // sets neither and reads "Reversed by staff" off reversed_by alone.
  replaced_by_sale_id: string | null;
  replaces_sale_id: string | null;
  // The linked sales' Confirmation references (#351): the "TP-X" the row
  // prints in "Corrected → TP-X" / "Corrects TP-Y". Null exactly when the
  // matching id is.
  replaced_by_confirmation_ref: string | null;
  replaces_confirmation_ref: string | null;
  // How many of the sale's Tickets have an accepted Holder: the people a
  // reversal tells. Stated on the row so the confirm dialog can say so first.
  held_ticket_count: number;
  // How the sale reached the platform (#370, ADR 0052): `sale_import`,
  // `manually_recorded`, `correction_replacement` or `channel_sale`. Derived by
  // the API off the row's own columns and stored nowhere — see saleOriginToken
  // for why this app is told the answer instead of working it out.
  origin: string;
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
// the platform's cut is never a number on this surface, ADR 0014), how many
// active Ticket Sales the Event has made across every channel, and how many
// tickets those sales moved.
//
// The last two are different questions and the strip shows both because one
// cannot be read off the other: a Customer who buys four tickets in one
// checkout is 1 sale and 4 Tickets Sold. Tickets Sold is the figure that
// answers how many people are coming.
export type EventSalesSummary = {
  net_proceeds_cents: number;
  currency: string;
  sales_count: number;
  tickets_sold: number;
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

// ReverseSaleResult mirrors POST /api/v1/staff/events/{id}/sales/{saleId}/reverse.
export type ReverseSaleResult = {
  sale_id: string;
  confirmation_ref: string;
  status: string;
  reversed_at: string;
  reversed_by: string;
};

/**
 * Whether a Sales list row offers Reverse (#350, ADR 0050).
 *
 * Three facts, all of which must hold: the viewer may manage the Event's sales
 * (the Sale Import's own gate — Event Staff see the state and no lever), the
 * sale is on the `import` channel (an Online Sale is the buyer's or the
 * platform's to reverse, and an In-Person Sale has no route yet), and it is
 * still active. The API refuses each of these too; this decides whether to show
 * a button, not whether the press would succeed.
 */
export function canReverseSale(
  canManageSales: boolean,
  sale: Pick<SaleListRow, "channel" | "status">,
): boolean {
  return canManageSales && sale.channel === "import" && sale.status === "active";
}

// reverseSale reverses one imported Ticket Sale via the BFF. Surfaces the API's
// refusal (SALE_NOT_IMPORTED, SALE_ALREADY_REVERSED, TICKET_SALE_NOT_FOUND) as
// ApiError for the catalog to word.
export async function reverseSale(eventId: string, saleId: string): Promise<ReverseSaleResult> {
  return fetchEventsJSON<ReverseSaleResult>(`/api/events/${eventId}/sales/${saleId}/reverse`, {
    method: "POST",
  });
}

// CorrectSaleInput is the Sale Correction form (#351, ADR 0050): the Sale
// Import template's columns for the replacement, plus send_confirmation.
export type CorrectSaleInput = {
  customer_email: string;
  customer_first_name: string;
  customer_last_name: string;
  customer_tax_id_type: string;
  customer_tax_id_number: string;
  ticket_type_id: string;
  quantity: number;
  payment_method: string;
  sold_at: string;
  amount_cents: number | null;
  send_confirmation: boolean;
};

// CorrectSaleResult mirrors POST /api/v1/staff/events/{id}/sales/{saleId}/correct.
export type CorrectSaleResult = {
  reversed_sale_id: string;
  reversed_confirmation_ref: string;
  replacement_sale_id: string;
  replacement_confirmation_ref: string;
  confirmation_sent: boolean;
};

/**
 * Whether a Sales list row offers Correct (#351, ADR 0050): the same three
 * facts as Reverse, because a Sale Correction is that reversal plus a
 * replacement, and nothing that cannot be reversed can be corrected.
 */
export function canCorrectSale(
  canManageSales: boolean,
  sale: Pick<SaleListRow, "channel" | "status">,
): boolean {
  return canReverseSale(canManageSales, sale);
}

/**
 * correctionPrefill is the Correct form's starting state, read off the row so
 * the Member retypes only what was wrong. The sold-at is cut to the minute in
 * the Event's own zone by the caller; here it is the row's ISO value.
 */
export function correctionPrefill(sale: SaleListRow): CorrectSaleInput {
  return {
    customer_email: sale.customer_email,
    customer_first_name: sale.customer_first_name,
    customer_last_name: sale.customer_last_name,
    customer_tax_id_type: sale.tax_id_type ?? "",
    customer_tax_id_number: sale.tax_id_number ?? "",
    ticket_type_id: sale.ticket_types[0]?.ticket_type_id ?? "",
    quantity: sale.ticket_types[0]?.quantity ?? 1,
    payment_method: sale.payment_method ?? "",
    sold_at: sale.sold_at,
    amount_cents: sale.amount_cents,
    send_confirmation: false,
  };
}

// correctSale records a Sale Correction via the BFF. A refused replacement
// arrives as ApiError with code VALIDATION_FAILED and a `fields` detail naming
// the template's columns; the other refusals (SALE_NOT_IMPORTED,
// SALE_ALREADY_REVERSED, TICKET_SALE_NOT_FOUND) as their own codes.
export async function correctSale(
  eventId: string,
  saleId: string,
  input: CorrectSaleInput,
): Promise<CorrectSaleResult> {
  return fetchEventsJSON<CorrectSaleResult>(`/api/events/${eventId}/sales/${saleId}/correct`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/**
 * previewSaleCorrection asks for the Correct form's live verdict (#352): the
 * same judgement the commit makes — the import row rules, capacity and the
 * Purchase Limit net of the sale being corrected, the duplicate warning — in
 * the Sale Import preview's shape, writing nothing. The commit's refusals
 * (SALE_NOT_IMPORTED, SALE_ALREADY_REVERSED, TICKET_SALE_NOT_FOUND) arrive as
 * ApiError.
 */
export async function previewSaleCorrection(
  eventId: string,
  saleId: string,
  input: CorrectSaleInput,
): Promise<ImportPreviewResult> {
  return fetchEventsJSON<ImportPreviewResult>(
    `/api/events/${eventId}/sales/${saleId}/correct/preview`,
    { method: "POST", body: JSON.stringify(input) },
  );
}

/** What the Correct form shows of a preview verdict, and whether it may commit. */
export type CorrectionVerdict = {
  // True when the commit would refuse: the row is invalid, a Ticket Type is
  // oversold, or the preview returned no row to judge.
  blocks: boolean;
  // The row's complaints by template column, first complaint per column.
  fieldErrors: Record<string, string>;
  // The sold-at date of another active sale the replacement matches, or null.
  // A warning: it never blocks.
  duplicateOfDate: string | null;
  // What the chosen Ticket Type has left once the old sale is reversed and the
  // replacement recorded, or null when the preview named no Ticket Type.
  remaining: { ticketTypeName: string; remaining: number } | null;
};

/**
 * correctionVerdict reads a preview into what the form needs to decide: the
 * commit is blocked on exactly what the commit would refuse (an invalid row or
 * an oversell), never on the duplicate warning, which is the organizer's call.
 */
export function correctionVerdict(result: ImportPreviewResult): CorrectionVerdict {
  const row = result.rows[0];
  if (!row) {
    return { blocks: true, fieldErrors: {}, duplicateOfDate: null, remaining: null };
  }
  const fieldErrors: Record<string, string> = {};
  for (const e of row.errors ?? []) {
    if (!(e.field in fieldErrors)) {
      fieldErrors[e.field] = e.message;
    }
  }
  const impact = result.capacity_impact[0];
  return {
    blocks: !row.valid || !result.committable,
    fieldErrors,
    duplicateOfDate: row.possible_duplicate && row.duplicate_of_date ? row.duplicate_of_date : null,
    remaining: impact
      ? { ticketTypeName: impact.ticket_type_name, remaining: impact.remaining - impact.requested }
      : null,
  };
}

// RecordSaleInput is the Manually Recorded Sale form's body (#369, ADR 0052):
// the Sale Import template's columns and nothing else. There is deliberately no
// send_confirmation — the buyer is always mailed, no prior Confirmation existing
// to fall back on — and no idempotency key, which is why the save button is
// disabled while a save is in flight.
export type RecordSaleInput = {
  customer_email: string;
  customer_first_name: string;
  customer_last_name: string;
  customer_tax_id_type: string;
  customer_tax_id_number: string;
  ticket_type_id: string;
  quantity: number;
  payment_method: string;
  sold_at: string;
  // Null uses the Ticket Type's catalog price. It is a price PER TICKET, as it
  // is in the uploaded template and in the Sale Correction; 0 records a comp.
  amount_cents: number | null;
};

// RecordedSale mirrors POST /api/v1/staff/events/{id}/sales, 201.
//
// possible_duplicate rides on the created sale rather than blocking it: the
// warning is the organizer's to overrule, and they are told about it after the
// fact as well as before.
export type RecordedSale = {
  sale_id: string;
  confirmation_ref: string;
  customer_email: string;
  customer_first_name: string;
  customer_last_name: string;
  ticket_type_id: string;
  ticket_type_name: string;
  quantity: number;
  amount_cents: number;
  currency: string;
  sold_at: string;
  payment_method: string;
  confirmation_sent: boolean;
  possible_duplicate: boolean;
  duplicate_of_date?: string;
};

/**
 * recordSale records one Manually Recorded Sale via the BFF (#369, ADR 0052).
 *
 * A refused row arrives as ApiError with code VALIDATION_FAILED and a `fields`
 * detail naming the template's bare columns; an Event that registers elsewhere
 * as EVENT_IS_EXTERNAL_REGISTRATION, and a capacity race lost between the
 * verdict and the write as IMPORT_BATCH_FAILED.
 */
export async function recordSale(eventId: string, input: RecordSaleInput): Promise<RecordedSale> {
  return fetchEventsJSON<RecordedSale>(`/api/events/${eventId}/sales`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/**
 * previewManualSale asks for the record-a-sale form's live verdict: the same
 * judgement the save makes — the import row rules, capacity, the Purchase Limit
 * and the duplicate warning — in the Sale Import preview's shape, writing
 * nothing.
 *
 * It answers 200 whatever the verdict, with ONE exception the caller must
 * handle: a quantity that is not a whole number is refused by the decoder as a
 * 400 VALIDATION_FAILED naming the quantity column, because a JSON 1.5 never
 * reaches the validator that would have worded it as a row verdict.
 */
export async function previewManualSale(
  eventId: string,
  input: RecordSaleInput,
): Promise<ImportPreviewResult> {
  return fetchEventsJSON<ImportPreviewResult>(`/api/events/${eventId}/sales/preview`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/**
 * rowFieldErrors reads the per-column complaints out of a VALIDATION_FAILED
 * refusal, keyed by the template's column name. Empty for any other error.
 *
 * Shared by every form that types ONE import row — the Sale Correction and the
 * Manually Recorded Sale — because both are refused the same way: bare template
 * column names, no `rows[N].` prefix.
 */
export function rowFieldErrors(details: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (!details || typeof details !== "object") {
    return out;
  }
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) {
    return out;
  }
  for (const f of fields) {
    if (f && typeof f === "object" && typeof (f as { field?: unknown }).field === "string") {
      const { field, message } = f as { field: string; message?: unknown };
      if (!(field in out)) {
        out[field] = typeof message === "string" ? message : "";
      }
    }
  }
  return out;
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

// exportFieldMessage is the field error's own sentence on a refused export, or
// null when the refusal carried none.
//
// It exists so the call site can PREFER it over everything else, and that is not
// a cosmetic preference: the row-cap refusal is a VALIDATION_FAILED, whose
// top-level message is the generic "Request validation failed", while the
// sentence that actually helps — how many sales matched, how many may be
// downloaded at once, and to narrow the filters — is the field error underneath
// it. Reading only the top level would replace the one useful message in the
// feature with boilerplate, and the cap is only survivable because the person is
// told which lever to pull.
//
// It stays the API's ENGLISH sentence rather than becoming a catalog key, and
// that is a deliberate trade rather than an oversight. The two numbers in it —
// how many matched and how many may travel — reach the app only inside the
// prose; the field error carries no `details` for `apiErrorMessage` to fill a
// Spanish sentence from. A translated "narrow your filters" would therefore be a
// Spanish sentence with the one actionable fact deleted, and ADR 0023's floor
// exists exactly for a refusal the catalog cannot state as well as the API can.
export function exportFieldMessage(details: unknown): string | null {
  if (typeof details !== "object" || details === null) return null;
  const fields = (details as { fields?: { message?: string }[] }).fields;
  if (!Array.isArray(fields)) return null;
  const message = fields.find((field) => field?.message)?.message;
  return typeof message === "string" && message.trim() !== "" ? message : null;
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
    // The envelope is re-thrown whole — message, code and details — rather than
    // pre-worded here. Choosing the sentence is the call site's job now: it
    // reads `exportFieldMessage` first, then the catalog by code, then its own
    // copy, and none of those three answers can be spelled in lib/.
    throw new ApiError(
      envelope?.error?.message ?? "",
      envelope?.error?.code,
      envelope?.error?.details as Record<string, unknown> | undefined,
    );
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

/* ────────────────────────────────────────────────────────────────────────────
 * TOKENS, NOT SENTENCES.
 *
 * Everything below used to hand the Sales list finished English: "In person",
 * "Cédula: 1712345678", "3 reversed sales", "Jul 7, 2026, 12:00 by Customer".
 * It now answers WHICH of a closed set of things is true and hands the facts
 * over, and messages/{en,es}.json says it in the reader's language (ADR 0041).
 *
 * The shape is the same one everywhere: a value the API sends is narrowed to a
 * token this app has a key for, or to null when it is a value this app has never
 * heard of. Null is the interesting half — a Sales Channel added by the backend
 * tomorrow must render as its raw value rather than as a blank cell, which is
 * the same floor ADR 0023 puts under an unknown error code — so the caller reads
 * `token ? t(KEY[token]) : raw`.
 *
 * Nothing here imports the catalog and nothing is handed a `t`, so this module
 * stays free of React and of the i18n runtime and `sales-api.test.ts` asserts a
 * decision rather than prose a copy edit would break.
 * ──────────────────────────────────────────────────────────────────────────── */

/** The Sales Channels a Ticket Sale can be recorded on. */
export const SALE_CHANNELS = ["online", "in_person", "import"] as const;
export type SaleChannel = (typeof SALE_CHANNELS)[number];

/** Where a sale on its channel came from. Absent on most channels. */
export const SALE_SOURCES = ["direct", "external_platform"] as const;
export type SaleSource = (typeof SALE_SOURCES)[number];

// The one place the Payment Method value set is spelled out for the staff app:
// the filter options and the row-detail labels both read from it. Values only —
// what each is called on screen is `sales.paymentCash` and its siblings.
export const PAYMENT_METHODS = ["cash", "transfer", "payphone"] as const;
export type PaymentMethod = (typeof PAYMENT_METHODS)[number];

/**
 * The Tax ID Types a sale can have been transacted under.
 *
 * Their names are Spanish in BOTH catalogs — Cédula, RUC, Pasaporte — because
 * they name Ecuadorian documents the buyer physically hands over, and an English
 * reader looking for a match against a passport is still looking at a document
 * that says "Cédula" on it. That is a fact about the world rather than a
 * translation gap, which is why the two catalogs agreeing here is correct.
 */
export const TAX_ID_TYPES = ["cedula", "ruc", "passport"] as const;
export type TaxIdType = (typeof TAX_ID_TYPES)[number];

/**
 * How a Ticket Sale reached the platform (#370, ADR 0052).
 *
 * DERIVED BY THE API, NOT HERE, and the split is deliberate. Recognising a
 * Manually Recorded Sale is a three-way negative — an imported sale with neither
 * a Sale Import batch nor a Sale Correction linkage — and ADR 0052 requires that
 * predicate to live in exactly ONE place, which is `sales.DeriveSaleOrigin` on
 * the backend. Restating it here from `channel` / `replaces_sale_id` would make
 * two places, and the second would be the one nobody updates when a third
 * batchless writer lands. This app is told the answer and narrows it to a token.
 *
 * The values are the API's own, one per route in:
 *
 * - `sale_import` — it arrived in an uploaded Sale Import batch.
 * - `manually_recorded` — somebody typed it into the form, one sale at a time.
 * - `correction_replacement` — it is a Sale Correction's replacement.
 * - `channel_sale` — it sold on a Sales Channel of its own, with no import
 *   behind it (an Online Sale today).
 */
export const SALE_ORIGINS = [
  "sale_import",
  "manually_recorded",
  "correction_replacement",
  "channel_sale",
] as const;
export type SaleOrigin = (typeof SALE_ORIGINS)[number];

// Who caused a Sale Reversal: the buyer undoing their own Online Sale inside the
// Reversal Window, their own staff undoing a Sale Import, or the platform
// recording a refund it made off-platform at the organization's request. The two
// answers a reversed row has to give are when and which side — and nothing of
// the platform's own money memo, which is operator-facing only (#125).
export const REVERSAL_ACTORS = ["customer", "staff", "operator"] as const;
export type ReversalActor = (typeof REVERSAL_ACTORS)[number];

/** narrow is the shared shape: a known value becomes a token, anything else null. */
function narrow<T extends string>(known: readonly T[], value: string | null | undefined): T | null {
  return value != null && (known as readonly string[]).includes(value) ? (value as T) : null;
}

/** Which Sales Channel a row was sold on, or null for one this app cannot name. */
export function saleChannelToken(channel: string | null): SaleChannel | null {
  return narrow(SALE_CHANNELS, channel);
}

/** Which source a row carries, or null when it has none this app can name. */
export function saleSourceToken(source: string | null): SaleSource | null {
  return narrow(SALE_SOURCES, source);
}

/**
 * How a row reached the platform, or null for an origin this app cannot name.
 *
 * Null is the load-bearing half. A fourth route — or a fourth batchless writer
 * on the `import` channel, which ADR 0052 warns is the way this set grows —
 * arrives here as a value that is not in the list, and the caller shows the
 * API's own word rather than a token it has no copy for. A guess would put a
 * confident, WRONG account of where a sale came from on the screen, which is
 * worse than an unfamiliar one: the whole point of the origin is to explain a
 * row nobody recognises. Null is also the answer for a payload carrying no
 * origin at all.
 */
export function saleOriginToken(origin: string | null | undefined): SaleOrigin | null {
  return narrow(SALE_ORIGINS, origin);
}

/** Which Payment Method a row was taken by, or null for one this app cannot name. */
export function paymentMethodToken(paymentMethod: string | null): PaymentMethod | null {
  return narrow(PAYMENT_METHODS, paymentMethod);
}

/**
 * A sale's Tax ID snapshot, or null on a sale recorded without one.
 *
 * Null on either half rather than half a Tax ID: a type without a number names
 * nothing and a number without a type is not confirmable against a document.
 * History is shown as it is and never backfilled (ADR 0016), so the caller
 * renders a plain dash rather than a placeholder.
 */
export type TaxIdSnapshot = {
  /** The Type as a key this app has copy for, or null for one it has not. */
  token: TaxIdType | null;
  /** The Type exactly as the API stated it — what a null token falls back to. */
  rawType: string;
  /** The number, unmasked: an organizer confirms a search match against it and
   * copies it into a declaration. */
  number: string;
};

export function taxIdSnapshot(
  taxIdType: string | null,
  taxIdNumber: string | null,
): TaxIdSnapshot | null {
  if (!taxIdType || !taxIdNumber) {
    return null;
  }
  return { token: narrow(TAX_ID_TYPES, taxIdType), rawType: taxIdType, number: taxIdNumber };
}

/**
 * What a reversed sale can say about how it came to be reversed.
 *
 * Three states rather than a nullable pair, because they are three different
 * sentences and the component should not be re-deriving which one applies:
 *
 * - `unrecorded` — reversed before the platform recorded either half (#117).
 *   It says so plainly rather than guessing a time.
 * - `when` — the moment is known and the side is not.
 * - `whenAndWho` — both, the ordinary case.
 */
export type ReversalProvenance =
  | { state: "unrecorded" }
  | { state: "when"; at: string }
  | { state: "whenAndWho"; at: string; actorToken: ReversalActor | null; rawActor: string };

export function reversalProvenance(
  reversedAt: string | null,
  reversedBy: string | null,
): ReversalProvenance {
  if (!reversedAt) {
    return { state: "unrecorded" };
  }
  if (!reversedBy) {
    return { state: "when", at: reversedAt };
  }
  return {
    state: "whenAndWho",
    at: reversedAt,
    actorToken: narrow(REVERSAL_ACTORS, reversedBy),
    rawActor: reversedBy,
  };
}
