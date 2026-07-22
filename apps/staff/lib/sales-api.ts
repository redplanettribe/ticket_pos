import { fetchEventsJSON } from "@/lib/events-api";

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

// fetchSalesList proxies the Sales list endpoint via the BFF for the given page.
export async function fetchSalesList(eventId: string, page: number): Promise<SalesListResponse> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(SALES_PAGE_SIZE),
  });
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
