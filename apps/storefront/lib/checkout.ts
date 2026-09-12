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
  // The Purchase Limit, or null/absent when this Ticket Type is unrestricted
  // (ADR 0025). Optional rather than required so a caller that predates the
  // limit reads as unrestricted instead of as a bound of zero: "not rationed"
  // is a statement, not a number to do arithmetic on.
  max_per_customer?: number | null;
  // How many of this Ticket Type the Customer READING the page already holds —
  // their active Ticket Sales plus their live Capacity Holds, the same count
  // begin-checkout refuses on and under the same name the API gives it
  // (ADR 0025, #168).
  //
  // Null or absent means we do not know who is asking, which is the anonymous
  // Event read and is NOT the same statement as zero. Zero says "you hold none
  // of these", and only a request that proved whose holdings it was asking about
  // can say that. So an absent figure subtracts nothing and leaves the bound
  // exactly where the Purchase Limit alone puts it — an anonymous visitor learns
  // of their allowance at submit, which is the first moment they have told us
  // who they are.
  already_held?: number | null;
  // The server's verdict on this Ticket Type's Sales Cutoff: true once the
  // closing instant an organizer set has passed, and the Ticket Type is still
  // listed, still described, still priced and no longer buyable (ADR 0070).
  //
  // Optional and read as false when absent, on max_per_customer's terms: a
  // caller that predates the cutoff has nothing closed, and no arithmetic is
  // done on it. Never folded into sold_out: capacity exhausted and time run out
  // are different facts and get different words on every surface.
  //
  // The verdict travels rather than being derived from an instant here, because
  // the server owns the clock: a browser set to yesterday must never be able to
  // offer a stepper the API will refuse.
  closed?: boolean;
};

/**
 * remainingAllowance is how many more of a Ticket Type one Customer may hold:
 * their Purchase Limit less what they already hold, floored at zero (ADR 0025).
 *
 * The floor is load-bearing rather than defensive. Lowering a Purchase Limit is
 * never retroactive — a Customer who bought five under a limit of five keeps all
 * five when the organizer drops the limit to two — so held EXCEEDING the limit is
 * a legitimate state, not a bug to assert against. Such a Customer is offered
 * zero, never a negative quantity that would flow into a stepper's bound.
 *
 * An unknown holding (the anonymous read) subtracts nothing: see already_held.
 */
function remainingAllowance(limit: number, alreadyHeld: number | null | undefined): number {
  if (alreadyHeld === null || alreadyHeld === undefined) return Math.max(limit, 0);
  return Math.max(limit - alreadyHeld, 0);
}

/**
 * offerableQuantity is the largest quantity the steppers may offer for one
 * Ticket Type: the smaller of what remains of the Event's stock and what remains
 * of this Customer's own allowance under the Purchase Limit (ADR 0025). An
 * absent Purchase Limit contributes no bound at all, so an unrestricted Ticket
 * Type offers exactly what remaining capacity allows — the behaviour every
 * Ticket Type had before limits existed.
 *
 * This is the Storefront's courtesy, not the rule: the API refuses a breach at
 * begin-checkout and is what actually holds it. Bounding here only spares an
 * honest buyer from meeting that refusal — which is precisely why the allowance
 * arm exists at all. Bounding at the bare limit offered a signed-in Customer
 * their whole limit again however much of it they had spent, so the page's one
 * job was undone the moment they had bought once (#165 left this open, #168
 * closes it).
 *
 * A limit higher than remaining capacity is not a licence to oversell, hence
 * min rather than either figure alone.
 */
export function offerableQuantity(ticketType: SellableTicketType): number {
  const capacityBound = Math.max(ticketType.remaining, 0);
  const limit = ticketType.max_per_customer;
  if (limit === null || limit === undefined) return capacityBound;
  return Math.min(capacityBound, remainingAllowance(limit, ticketType.already_held));
}

/**
 * allowanceSpent is whether THIS Customer has used up their Purchase Limit on a
 * Ticket Type that is otherwise still on sale — the one state the Event page has
 * to word differently from sold out (ADR 0025, #168).
 *
 * It is deliberately not "offerableQuantity is zero": that is true of a sold-out
 * Ticket Type too, and conflating them is the exact failure this exists to
 * prevent — a Customer must never read their own spent allowance as the Event
 * being full. Sold out is therefore excluded here and keeps its own wording,
 * because it is a fact about the Event and everyone reading the page sees it.
 *
 * Closed is excluded on exactly those grounds, and tested apart from sold_out
 * rather than folded into it, as clampQuantity tests them (ADR 0070, #621).
 * Sales having ended is a fact about the Event too, it already carries its own
 * badge, and the badge ranking puts it above limit-reached precisely so the
 * card stops inviting a purchase nobody can make. Saying in the next breath
 * that nothing is sold out and this is only the reader's own limit contradicts
 * that badge and offers a way round a wall that has no way round it. The
 * allowance is still spent, arithmetically; it has simply stopped being the
 * reason this Customer cannot buy, so it stops being what the card says.
 * Absent reads as open, so a Ticket Type nobody has typed a date into words
 * a spent allowance exactly as it did before the Sales Cutoff existed.
 *
 * False whenever already_held is absent, which is every anonymous read: an
 * unknown holding cannot have exhausted anything, and treating it as zero
 * spent would tell a visitor something about themselves we do not know.
 */
export function allowanceSpent(ticketType: SellableTicketType): boolean {
  if (ticketType.sold_out) return false;
  if (ticketType.closed) return false;
  const limit = ticketType.max_per_customer;
  const alreadyHeld = ticketType.already_held;
  if (limit === null || limit === undefined) return false;
  if (alreadyHeld === null || alreadyHeld === undefined) return false;
  return remainingAllowance(limit, alreadyHeld) === 0;
}

/**
 * clampQuantity keeps a stepper's value honest: an integer between zero and
 * what the Ticket Type may offer. Anything unparseable is zero, and neither a
 * sold-out nor a closed type can hold a quantity at all.
 *
 * The closed gate arrived with the closed card (#607) and not before it. While
 * the steppers were still drawn for a closed Ticket Type, returning zero here
 * would have left a `+` that was clickable and did nothing — worse for a buyer
 * than the honest refusal the API already gives them. Withdrawing the control is
 * what makes zero the right answer, and putting the rule here rather than at
 * each call site is what stops the two from drifting: restoreSelection used to
 * carry its own copy of it and now inherits this one, so a basket restored from
 * an address cannot bring back a line begin-checkout would refuse (ADR 0070).
 *
 * Closed is tested apart from sold_out and never folded into it: a closed Ticket
 * Type may have plenty of stock left, and the two states get different words on
 * every surface. Absent reads as open, so a Ticket Type nobody has typed a date
 * into clamps exactly as it did before this feature existed.
 */
export function clampQuantity(raw: number, ticketType: SellableTicketType): number {
  if (ticketType.sold_out) return 0;
  if (ticketType.closed) return 0;
  if (!Number.isFinite(raw)) return 0;
  const whole = Math.trunc(raw);
  if (whole <= 0) return 0;
  return Math.min(whole, offerableQuantity(ticketType));
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

/**
 * checkoutDestination decides where a begun checkout sends the buyer. A cart
 * with money to collect goes to the Payment Provider's hosted page; one that
 * cost nothing settled on the spot and goes straight to the terminal success
 * page — the same place the provider return leg lands (ADR 0017).
 *
 * Returns null when the API said neither, because there is nowhere honest to
 * go. Navigating to a redirect_url that was never sent is what put buyers on
 * `/{orgSlug}/events/undefined`: the browser resolves the string "undefined"
 * against the event page it came from.
 */
export function checkoutDestination(result: {
  status?: string;
  redirect_url?: string;
  confirmation_ref?: string;
}): string | null {
  if (result.status === "approved" && result.confirmation_ref) {
    return `/checkout/success?ref=${encodeURIComponent(result.confirmation_ref)}`;
  }
  const redirect = result.redirect_url?.trim();
  return redirect ? redirect : null;
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
