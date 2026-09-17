/**
 * The Customer Dossier as the staff surface reads it (#635, #638; CONTEXT.md
 * "Customer Dossier", Ficha del cliente): everything one Event knows about one
 * Customer, addressed by the Customer's id and never by their email.
 *
 * SCOPED TO THE EVENT. The name and Tax ID arrive on each Sale because they are
 * what was given on THAT Sale — this Event's own snapshots — and never what the
 * platform-global Customer record says today. Nothing here joins or dedupes
 * them across Sales; two Sales with two Tax IDs show two Tax IDs.
 *
 * Later tickets widen `DossierSale` (#639) and add Tickets and Answers beside
 * `sales` (#640, #641). Pure apart from the fetch, so it runs under node --test.
 */

import { ApiError } from "./events-api.ts";
import type { SaleTicketType } from "./sales-api.ts";

/*
  THE READ
*/

/**
 * A Sale's standing. `corrected` is a Sale reversed by a Sale Correction, told
 * apart by the API so this side need not infer it from the replacement ref.
 */
export type DossierSaleStatus = "active" | "reversed" | "corrected";

/** One of this Event's Ticket Sales to this Customer, as given on that Sale. */
export type DossierSale = {
  id: string;
  confirmation_ref: string;
  /** A string on the wire: narrow it with `saleStatusToken` before drawing. */
  status: string;
  /** When it was reversed; set for `reversed` and `corrected`. */
  reversed_at: string | null;
  /** The Sale Correction's replacement, on a `corrected` Sale. */
  replaced_by_confirmation_ref: string | null;
  sold_at: string;
  recorded_at: string;
  channel: string;
  source: string | null;
  origin: string;
  ticket_types: SaleTicketType[];
  amount_cents: number;
  currency: string;
  payment_method: string | null;
  customer_first_name: string;
  customer_last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
  /*
    What surrounds the Sale (#639); decided by `lib/dossier-sale-surroundings`.
  */
  /** The phone given on this Sale's checkout, off its Payment (ADR 0073); null when none was. */
  phone: string | null;
  /** The display name of the Affiliate Link that attributed the Sale, or null. */
  affiliate_link_name: string | null;
  /** The Sale's Tax Invoices in chain order; empty, never null. */
  tax_invoices: DossierTaxInvoice[];
  /** When the Sale was re-addressed (ADR 0058), or null. Never either address. */
  re_addressed_at: string | null;
};

/** One Tax Invoice about a Dossier Sale. Strings on the wire: narrow before drawing. */
export type DossierTaxInvoice = {
  kind: string;
  role: string;
  /** Null until signed. */
  number: string | null;
  status: string;
  recipient_legal_name: string;
  recipient_tax_id_type: string;
  recipient_tax_id: string;
};

export type CustomerDossier = {
  customer: { id: string; email: string };
  /** Newest `sold_at` first, as the API orders them. */
  sales: DossierSale[];
};

/**
 * What loading a Dossier can come to, short of a thrown failure.
 *
 * NOT FOUND IS AN OUTCOME AND NOT AN ERROR. The API answers 404 alike for an
 * Event outside the caller's Organization and for a Customer with no Sale on
 * this Event, and the page says one plain sentence for both. Read off the HTTP
 * status rather than an error code, so the sentence does not depend on which
 * `*_NOT_FOUND` code the API happens to choose.
 */
export type CustomerDossierResult =
  | { state: "found"; dossier: CustomerDossier }
  | { state: "not_found" };

/** Reads one Customer's Dossier on one Event through the BFF route. */
export async function fetchCustomerDossier(
  eventId: string,
  customerId: string,
): Promise<CustomerDossierResult> {
  const response = await fetch(
    `/api/events/${encodeURIComponent(eventId)}/customers/${encodeURIComponent(customerId)}/dossier`,
    { headers: { "Content-Type": "application/json" } },
  );
  if (response.status === 404) {
    return { state: "not_found" };
  }
  const envelope = (await response.json().catch(() => null)) as {
    data?: CustomerDossier | null;
    error?: { message?: string; code?: string; details?: Record<string, unknown> } | null;
  } | null;
  if (!response.ok || envelope?.error || !envelope?.data) {
    throw new ApiError(
      envelope?.error?.message ?? "",
      envelope?.error?.code,
      envelope?.error?.details,
    );
  }
  return { state: "found", dossier: envelope.data };
}

/*
  THE ADDRESS, AND THE WAY BACK
*/

/** The Sales list: where Back goes when `from` cannot be trusted. */
function salesListPath(eventId: string): string {
  return `/events/${encodeURIComponent(eventId)}/sales`;
}

/**
 * The Dossier's own address. The Customer id is the only thing naming the
 * person, so no email reaches history, a pasted link or a log (#635 story 8).
 * `from` is the list the reader came from, filters and all, for Back.
 */
export function dossierHref(eventId: string, customerId: string, from: string): string {
  return `${salesListPath(eventId)}/customers/${encodeURIComponent(customerId)}?from=${encodeURIComponent(from)}`;
}

// A dot segment, literal or percent-encoded: a URL parser resolves `%2e%2e`
// exactly as it resolves `..`, so both would walk out of the Event.
const DOT_SEGMENT = /^(?:\.|%2e){1,2}$/i;
// C0 controls and DEL. A browser strips tabs and newlines from a URL, which is
// how `/\t/evil.example` becomes `//evil.example`.
const CONTROL_CHARACTER = /[\u0000-\u001f\u007f]/;

/**
 * Where Back goes: `from`, when it is a relative path within this Event's own
 * staff pages, and the Sales list otherwise.
 *
 * `from` is whatever a URL said, so it is an OPEN REDIRECT unless held to this:
 * it must begin `/events/<eventId>` and continue with `/`, `?`, `#` or nothing,
 * which rules out absolute URLs, schemes, protocol-relative `//host`, another
 * Event and an Event whose id merely starts with this one's. Backslashes and
 * control characters are refused because browsers rewrite them into slashes or
 * drop them, and dot segments because they climb out of the prefix after it has
 * been checked. `from` arrives already decoded from the search params.
 */
export function dossierBackHref(from: string | null | undefined, eventId: string): string {
  const fallback = salesListPath(eventId);
  if (typeof from !== "string" || from === "" || eventId === "") {
    return fallback;
  }
  if (from.includes("\\") || CONTROL_CHARACTER.test(from)) {
    return fallback;
  }
  const prefix = `/events/${encodeURIComponent(eventId)}`;
  if (!from.startsWith(prefix)) {
    return fallback;
  }
  const next = from.charAt(prefix.length);
  if (next !== "" && next !== "/" && next !== "?" && next !== "#") {
    return fallback;
  }
  const path = from.split(/[?#]/, 1)[0];
  if (path.split("/").some((segment) => DOT_SEGMENT.test(segment))) {
    return fallback;
  }
  return from;
}

/*
  LABELLING
*/

export const DOSSIER_SALE_STATUSES = ["active", "reversed", "corrected"] as const satisfies readonly DossierSaleStatus[];

/**
 * A Sale's status as a token this app has words for, or null for one it has
 * never heard of — the caller then shows the API's own word rather than
 * guessing, the same floor `saleChannelToken` keeps (ADR 0041).
 */
export function saleStatusToken(status: string | null | undefined): DossierSaleStatus | null {
  return status != null && (DOSSIER_SALE_STATUSES as readonly string[]).includes(status)
    ? (status as DossierSaleStatus)
    : null;
}

/**
 * The Badge variant a status is drawn in. A Sale that no longer stands is
 * destructive, as on the sales list; an unrecognised status is drawn quietly
 * rather than as a live Sale, because nothing says it is one.
 */
export function saleStatusBadgeVariant(
  token: DossierSaleStatus | null,
): "secondary" | "destructive" | "outline" {
  switch (token) {
    case "active":
      return "secondary";
    case "reversed":
    case "corrected":
      return "destructive";
    default:
      return "outline";
  }
}

/**
 * The name given on one Sale, the two halves joined, or "" when neither was
 * given — so the caller draws its "nothing to show" rather than a stray space.
 */
export function nameGivenOnSale(sale: Pick<DossierSale, "customer_first_name" | "customer_last_name">): string {
  return [sale.customer_first_name, sale.customer_last_name]
    .map((part) => (part ?? "").trim())
    .filter(Boolean)
    .join(" ");
}
