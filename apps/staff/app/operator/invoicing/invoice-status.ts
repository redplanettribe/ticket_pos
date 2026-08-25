import type { InvoiceStatus } from "@/lib/operator-api";

// The Tax Invoice status vocabulary, mapped once: the list and the detail both
// draw a status badge, and one word and one colour per state is how there
// stays one Spanish word for "Not authorized". Written the way
// app/payout-request-status.ts is, and for the same reason.

export const INVOICE_STATUS_KEYS = {
  pending: "invoicingStatusPending",
  authorized: "invoicingStatusAuthorized",
  not_authorized: "invoicingStatusNotAuthorized",
  rejected: "invoicingStatusRejected",
} as const satisfies Record<InvoiceStatus, string>;

export const INVOICE_STATUS_VARIANTS: Record<InvoiceStatus, "default" | "secondary" | "destructive"> = {
  pending: "secondary",
  authorized: "default",
  not_authorized: "destructive",
  rejected: "destructive",
};
