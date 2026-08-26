import type { InvoiceKind, InvoiceStatus } from "@/lib/operator-api";

// The Tax Invoice status vocabulary, mapped once: the list and the detail both
// draw a status badge, and one word and one colour per state is how there
// stays one Spanish word for "Not authorized". Written the way
// app/payout-request-status.ts is, and for the same reason. The four states
// of a document the platform owes itself (#473) sit beside the manual four.

export const INVOICE_STATUS_KEYS = {
  owed: "invoicingStatusOwed",
  pending: "invoicingStatusPending",
  authorized: "invoicingStatusAuthorized",
  not_authorized: "invoicingStatusNotAuthorized",
  rejected: "invoicingStatusRejected",
  needs_attention: "invoicingStatusNeedsAttention",
  withdrawn: "invoicingStatusWithdrawn",
  annulled: "invoicingStatusAnnulled",
} as const satisfies Record<InvoiceStatus, string>;

export const INVOICE_STATUS_VARIANTS: Record<InvoiceStatus, "default" | "secondary" | "destructive" | "outline"> = {
  owed: "secondary",
  pending: "secondary",
  authorized: "default",
  not_authorized: "destructive",
  rejected: "destructive",
  needs_attention: "destructive",
  withdrawn: "outline",
  annulled: "outline",
};

// The document kind, one word each: what the list's Kind column and the
// detail's badge say.
export const INVOICE_KIND_KEYS = {
  manual: "invoicingKindManual",
  sale: "invoicingKindSale",
  credit_note: "invoicingKindCreditNote",
} as const satisfies Record<InvoiceKind, string>;
