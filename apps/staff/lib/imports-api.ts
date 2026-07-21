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
  customer_name: string;
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
    throw new ApiError(envelope.error?.message ?? "Request failed", envelope.error?.code, envelope.error?.details);
  }
  if (envelope.data === null) {
    throw new Error("Empty response");
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

export function formatBatchTimestamp(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
