/**
 * What surrounds a Sale on the Customer Dossier (#639): the phone given on its
 * checkout (ADR 0073), the Affiliate Link that attributed it, its Sale
 * Invoices and when it was re-addressed (ADR 0058). The words these draw live
 * in the `customerDossier` catalog; this module decides which one, so the
 * decisions run under node --test.
 *
 * The API decides every fact — which phone, which documents in which order and
 * role. Nothing here re-derives them; unknown values are shown as the API's
 * own word rather than guessed at (ADR 0041).
 */

/*
  PHONE
*/

/**
 * The phone a Sale's checkout was given, or that none was. `href` dials it —
 * the Dossier is opened on a phone at the door — and is null when the value
 * holds nothing dialable, which is then shown as plain text.
 */
export type PhoneGiven = { state: "given"; phone: string; href: string | null } | { state: "none" };

export function phoneGivenOnSale(sale: { phone?: string | null }): PhoneGiven {
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
  SALE INVOICES
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
] as const;

export type DossierInvoiceStatus = (typeof DOSSIER_INVOICE_STATUSES)[number];

const INVOICE_STATUS_KEYS = {
  owed: "invoiceStatusOwed",
  pending: "invoiceStatusPending",
  authorized: "invoiceStatusAuthorized",
  not_authorized: "invoiceStatusNotAuthorized",
  rejected: "invoiceStatusRejected",
  needs_attention: "invoiceStatusNeedsAttention",
  withdrawn: "invoiceStatusWithdrawn",
  annulled: "invoiceStatusAnnulled",
  abandoned: "invoiceStatusAbandoned",
} as const satisfies Record<DossierInvoiceStatus, string>;

export type DossierInvoiceStatusKey = (typeof INVOICE_STATUS_KEYS)[DossierInvoiceStatus];

function isInvoiceStatus(status: string | null | undefined): status is DossierInvoiceStatus {
  return status != null && (DOSSIER_INVOICE_STATUSES as readonly string[]).includes(status);
}

/** The catalog key for a document's status, or null for one never heard of. */
export function dossierInvoiceStatusKey(status: string | null | undefined): DossierInvoiceStatusKey | null {
  return isInvoiceStatus(status) ? INVOICE_STATUS_KEYS[status] : null;
}

/**
 * The Badge variant a document's status is drawn in, as the operator's Tax
 * Invoices list draws it: authorized stands out, a refusal is trouble, a dead
 * document is quiet. An unknown status is drawn quietly.
 */
export function dossierInvoiceStatusVariant(
  status: string | null | undefined,
): "default" | "secondary" | "destructive" | "outline" {
  if (!isInvoiceStatus(status)) {
    return "outline";
  }
  switch (status) {
    case "authorized":
      return "default";
    case "owed":
    case "pending":
      return "secondary";
    case "not_authorized":
    case "rejected":
    case "needs_attention":
      return "destructive";
    default:
      return "outline";
  }
}

const INVOICE_KIND_KEYS: Record<string, "invoiceKindSale" | "invoiceKindCreditNote"> = {
  sale: "invoiceKindSale",
  credit_note: "invoiceKindCreditNote",
};

/** The catalog key for a document's kind, or null for one never heard of. */
export function dossierInvoiceKindKey(kind: string): "invoiceKindSale" | "invoiceKindCreditNote" | null {
  return Object.hasOwn(INVOICE_KIND_KEYS, kind) ? INVOICE_KIND_KEYS[kind] : null;
}

/**
 * The catalog key a document's role is badged with, or null where the kind and
 * status already say it — a Credit Note is its kind, a withdrawn or annulled
 * factura its status — as the operator Sale lookup badges them.
 */
export function dossierInvoiceRoleKey(role: string): "invoiceRoleCurrent" | "invoiceRoleSuperseded" | null {
  switch (role) {
    case "current":
      return "invoiceRoleCurrent";
    case "superseded":
      return "invoiceRoleSuperseded";
    default:
      return null;
  }
}
