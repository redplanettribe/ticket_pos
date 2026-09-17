/**
 * What surrounds a Sale on the Customer Dossier (#639): the phone given on its
 * checkout (ADR 0073), the Affiliate Link that attributed it, its Tax Invoices
 * and when it was re-addressed (ADR 0058).
 *
 * The API decides every fact — which phone, which documents in which order and
 * role. A document's kind, status and role are drawn with the operator Tax
 * Invoice vocabulary (`app/operator/invoicing/invoice-status.ts`,
 * `lib/sale-documents.ts`) so one state keeps one word; this module only
 * narrows the Dossier's wire strings onto it, and an unknown value is shown as
 * the API's own word rather than guessed at (ADR 0041).
 */

import type { InvoiceKind, InvoiceStatus, OperatorDocumentRole } from "./operator-api";

/*
  PHONE
*/

/**
 * The phone a Sale's checkout was given, or that none was. `href` dials it —
 * the Dossier is opened on a phone at the door — and is null when the value
 * holds nothing dialable, which is then shown as plain text.
 */
export type PhoneGiven = { state: "given"; phone: string; href: string | null } | { state: "none" };

export function phoneGivenOnSale(sale: { phone: string | null }): PhoneGiven {
  const phone = (sale.phone ?? "").trim();
  if (phone === "") {
    return { state: "none" };
  }
  const digits = phone.replace(/[^\d]/g, "");
  if (digits === "") {
    return { state: "given", phone, href: null };
  }
  return { state: "given", phone, href: `tel:${phone.startsWith("+") ? "+" : ""}${digits}` };
}

/*
  TAX INVOICES
*/

export const DOSSIER_INVOICE_STATUSES = [
  "owed",
  "pending",
  "authorized",
  "not_authorized",
  "rejected",
  "needs_attention",
  "withdrawn",
  "annulled",
  "abandoned",
] as const satisfies readonly InvoiceStatus[];

const INVOICE_KINDS = ["manual", "sale", "credit_note"] as const satisfies readonly InvoiceKind[];

const DOCUMENT_ROLES = ["current", "superseded", "credit_note", "not_current"] as const satisfies readonly OperatorDocumentRole[];

function narrow<T extends string>(known: readonly T[], value: string | null | undefined): T | null {
  return value != null && (known as readonly string[]).includes(value) ? (value as T) : null;
}

/** A document's status as one the operator vocabulary has words for, or null. */
export function invoiceStatusToken(status: string | null | undefined): InvoiceStatus | null {
  return narrow<InvoiceStatus>(DOSSIER_INVOICE_STATUSES, status);
}

/** A document's kind as one the operator vocabulary has words for, or null. */
export function invoiceKindToken(kind: string | null | undefined): InvoiceKind | null {
  return narrow<InvoiceKind>(INVOICE_KINDS, kind);
}

/** A document's role in its Sale's chain, or null for one never heard of. */
export function documentRoleToken(role: string | null | undefined): OperatorDocumentRole | null {
  return narrow<OperatorDocumentRole>(DOCUMENT_ROLES, role);
}
