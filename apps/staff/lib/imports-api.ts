import { ApiError, type APIEnvelope } from "@/lib/events-api";

// Types mirror the Go Sale Import response shapes (internal/sales). Field names
// match the JSON the API emits so the preview verdicts render verbatim.

export type ImportRowError = {
  field: string;
  message: string;
};

export type ImportPreviewRow = {
  row: number;
  customer_email: string;
  customer_first_name: string;
  customer_last_name: string;
  ticket_type: string;
  ticket_type_id?: string;
  ticket_type_name?: string;
  quantity: number;
  payment_method: string;
  sold_at?: string;
  amount_cents?: number;
  valid: boolean;
  errors?: ImportRowError[];
  possible_duplicate?: boolean;
  duplicate_of_date?: string;
};

export type ImportCapacityImpact = {
  ticket_type_id: string;
  ticket_type_name: string;
  requested: number;
  sold_count: number;
  capacity: number;
  remaining: number;
  overage: number;
  oversold: boolean;
};

export type ImportPreviewResult = {
  rows: ImportPreviewRow[];
  capacity_impact: ImportCapacityImpact[];
  valid_rows: number;
  total_rows: number;
  committable: boolean;
};

export type ImportCommitResult = {
  batch_id: string;
  sale_count: number;
  status: string;
  replayed: boolean;
};

export type ImportUndoResult = {
  batch_id: string;
  sale_count: number;
  status: string;
  notified: boolean;
};

export type ImportHistoryEntry = {
  batch_id: string;
  created_at: string;
  sale_count: number;
  source: string;
  status: string;
  actor_member_id?: string;
  actor_email?: string;
};

// postImportForm sends a multipart batch to a BFF route and unwraps the JSON
// envelope, surfacing the API error code (e.g. IMPORT_BATCH_FAILED) as ApiError.
async function postImportForm<T>(path: string, form: FormData): Promise<T> {
  const response = await fetch(path, { method: "POST", body: form });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    // No English stand-in for a missing message. The envelope is re-thrown as it
    // arrived and the SURFACE picks the words: its catalog by error code, then
    // its own sentence when the envelope carried none (ADR 0023). A "Request
    // failed" invented here would be an English sentence the catalog could never
    // outrank, because `apiErrorMessage` cannot tell it from the API's own.
    throw new ApiError(envelope.error?.message ?? "", envelope.error?.code, envelope.error?.details);
  }
  if (envelope.data === null) {
    // A 200 with no data is a broken API rather than a refusal, so it carries no
    // code and no reader-facing sentence: the surface shows its own copy for
    // "this did not work", in the reader's language.
    throw new ApiError("");
  }
  return envelope.data;
}

export async function previewSaleImport(eventId: string, file: File): Promise<ImportPreviewResult> {
  const form = new FormData();
  form.append("file", file);
  return postImportForm<ImportPreviewResult>(`/api/events/${eventId}/sale-imports/preview`, form);
}

export async function commitSaleImport(
  eventId: string,
  file: File,
  idempotencyKey: string,
  skipRows: number[],
): Promise<ImportCommitResult> {
  const form = new FormData();
  form.append("file", file);
  form.append("idempotency_key", idempotencyKey);
  form.append("source", "direct");
  if (skipRows.length > 0) {
    form.append("skip_rows", skipRows.join(","));
  }
  return postImportForm<ImportCommitResult>(`/api/events/${eventId}/sale-imports`, form);
}

// undoSaleImport reverses the latest Sale Import batch via the BFF, opting into
// buyer void notices when notifyBuyers is set. Surfaces the API error code
// (e.g. IMPORT_NOT_LATEST_BATCH, IMPORT_ALREADY_REVERSED) as ApiError.
export async function undoSaleImport(
  eventId: string,
  batchId: string,
  notifyBuyers: boolean,
): Promise<ImportUndoResult> {
  const response = await fetch(`/api/events/${eventId}/sale-imports/${batchId}/undo`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ notify_buyers: notifyBuyers }),
  });
  const envelope = (await response.json()) as APIEnvelope<ImportUndoResult>;
  if (!response.ok || envelope.error) {
    // No English stand-in for a missing message. The envelope is re-thrown as it
    // arrived and the SURFACE picks the words: its catalog by error code, then
    // its own sentence when the envelope carried none (ADR 0023). A "Request
    // failed" invented here would be an English sentence the catalog could never
    // outrank, because `apiErrorMessage` cannot tell it from the API's own.
    throw new ApiError(envelope.error?.message ?? "", envelope.error?.code, envelope.error?.details);
  }
  if (envelope.data === null) {
    // A 200 with no data is a broken API rather than a refusal, so it carries no
    // code and no reader-facing sentence: the surface shows its own copy for
    // "this did not work", in the reader's language.
    throw new ApiError("");
  }
  return envelope.data;
}

/*
 * There was a `formatBatchTimestamp(iso)` here until #289. It called
 * `Intl.DateTimeFormat(undefined, …)`, which is not English and not the
 * platform's zone: it is the BROWSER's language and the LAPTOP's zone, neither
 * of which this application chose. An Org Admin in Guayaquil on a machine set to
 * Europe/Madrid read every import in her import history as having happened seven
 * hours later than it did.
 *
 * Both halves now come from somewhere that had to be stated. The import history
 * renders `formatDateTime(entry.created_at, timezone, locale)` from
 * lib/format.ts, with the Event's own timezone passed down from the Sales page
 * and PLATFORM_TIME_ZONE beneath it — never the reader's machine — and the marks
 * from the reader's Staff Locale.
 */
