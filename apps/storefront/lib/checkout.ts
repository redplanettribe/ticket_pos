/**
 * Pure checkout logic: ticket selection arithmetic on the event page, and the
 * parsing/building of the URLs the Payment Provider redirect legs carry.
 *
 * Everything here is framework-free so it can be unit tested with node:test
 * (like destination.ts and google-signin.ts). The components and route handlers
 * own the I/O; this module owns the decisions.
 */

/** One requested Ticket Type and quantity, as the begin-checkout API takes it. */
export type CheckoutLine = {
  ticket_type_id: string;
  quantity: number;
};

/** What the steppers need to know about one Ticket Type to sell it. */
export type SellableTicketType = {
  id: string;
  price_cents: number;
  remaining: number;
  sold_out: boolean;
};

/**
 * clampQuantity keeps a stepper's value honest: an integer between zero and
 * what remains of the Ticket Type. Anything unparseable is zero, and a sold-out
 * type can never hold a quantity at all.
 */
export function clampQuantity(raw: number, ticketType: SellableTicketType): number {
  if (ticketType.sold_out) return 0;
  if (!Number.isFinite(raw)) return 0;
  const whole = Math.trunc(raw);
  if (whole <= 0) return 0;
  return Math.min(whole, Math.max(ticketType.remaining, 0));
}

/** Total number of tickets selected across every Ticket Type. */
export function totalQuantity(selection: Record<string, number>): number {
  return Object.values(selection).reduce((sum, quantity) => sum + quantity, 0);
}

/**
 * totalCents prices the selection at the quantities currently chosen. The API
 * re-prices at begin-checkout from its own catalog — this figure is what the
 * running total shows, not what is charged.
 */
export function totalCents(
  ticketTypes: SellableTicketType[],
  selection: Record<string, number>,
): number {
  return ticketTypes.reduce(
    (sum, ticketType) => sum + (selection[ticketType.id] ?? 0) * ticketType.price_cents,
    0,
  );
}

/**
 * selectionLines turns the stepper state into begin-checkout lines, dropping
 * zero quantities so a zero-quantity checkout is impossible by construction.
 */
export function selectionLines(selection: Record<string, number>): CheckoutLine[] {
  return Object.entries(selection)
    .filter(([, quantity]) => quantity > 0)
    .map(([ticket_type_id, quantity]) => ({ ticket_type_id, quantity }));
}

// --- Stub Payment Provider interstitial (dev only) -------------------------
//
// Contract documented in backend/internal/platform/payment.go: the stub's
// "hosted payment page" is {STOREFRONT_BASE_URL}/checkout/stub with
// client_transaction_id, amount_cents, currency, and response_url in the
// query; Approve/Decline navigate to response_url with client_transaction_id
// and outcome=approved|declined appended.

/** What the stub interstitial needs from its query string. */
export type StubPaymentRequest = {
  clientTransactionId: string;
  amountCents: number;
  currency: string;
  responseUrl: string;
};

/**
 * parseStubPaymentRequest reads the interstitial's query params, refusing
 * anything malformed: a missing id, a non-integer amount, or a response URL
 * that is not plain http(s). The stub only ever receives URLs the API built,
 * so anything else is a hand-crafted address and gets a 404, not a guess.
 */
export function parseStubPaymentRequest(params: {
  client_transaction_id?: string;
  amount_cents?: string;
  currency?: string;
  response_url?: string;
}): StubPaymentRequest | null {
  const clientTransactionId = params.client_transaction_id?.trim() ?? "";
  const currency = params.currency?.trim() ?? "";
  const rawAmount = params.amount_cents?.trim() ?? "";
  const responseUrl = params.response_url?.trim() ?? "";

  if (!clientTransactionId || !currency || !/^\d+$/.test(rawAmount)) {
    return null;
  }
  const amountCents = Number.parseInt(rawAmount, 10);
  if (!Number.isSafeInteger(amountCents)) {
    return null;
  }
  let parsed: URL;
  try {
    parsed = new URL(responseUrl);
  } catch {
    return null;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return null;
  }
  return { clientTransactionId, amountCents, currency, responseUrl };
}

/** The two verdicts the stub interstitial can hand back. */
export type StubOutcome = "approved" | "declined";

/**
 * stubOutcomeURL builds the return-redirect leg: response_url with
 * client_transaction_id and outcome appended, preserving whatever query the
 * response URL already carried.
 */
export function stubOutcomeURL(
  responseUrl: string,
  clientTransactionId: string,
  outcome: StubOutcome,
): string {
  const url = new URL(responseUrl);
  url.searchParams.set("client_transaction_id", clientTransactionId);
  url.searchParams.set("outcome", outcome);
  return url.toString();
}

/**
 * parseProviderReturn reads a Payment Provider's return redirect. Each
 * provider names our client transaction id differently — the stub sends
 * `client_transaction_id`, PayPhone appends `clientTransactionId` (and `id`)
 * to the response URL — so both spellings are accepted, and every query param
 * is relayed verbatim as provider_params for the provider's Confirm call.
 */
export function parseProviderReturn(params: URLSearchParams): {
  clientTransactionId: string;
  providerParams: Record<string, string>;
} {
  const clientTransactionId =
    params.get("client_transaction_id")?.trim() ?? params.get("clientTransactionId")?.trim() ?? "";
  const providerParams: Record<string, string> = {};
  for (const [key, value] of params.entries()) {
    providerParams[key] = value;
  }
  return { clientTransactionId, providerParams };
}

// --- Checkout context ------------------------------------------------------

/**
 * safeEventPath validates an event-page path recovered from the checkout
 * context cookie before it becomes an href: it must be a plain absolute path
 * on this Storefront (mirroring destination.safeNext, but with no default —
 * a missing context simply means no event link).
 */
export function safeEventPath(path: string | null | undefined): string | null {
  if (!path || !path.startsWith("/") || path.startsWith("//")) {
    return null;
  }
  return path;
}
