/**
 * A Ticket Type's Purchase Limit as the staff editor reasons about it (ADR
 * 0024): the most of that Ticket Type a single Customer may hold at once,
 * unset on most Ticket Types. The wire field is `max_per_customer`, a nullable
 * positive integer, and null says "not rationed" rather than inviting
 * arithmetic on a sentinel.
 *
 * The parse lives here rather than inline in the form because the create and
 * the update handler must agree on it exactly, and because the empty case is
 * the trap: an optional numeric field cannot reuse the required-field
 * `Number.parseInt` guard, since `Number.parseInt("", 10)` is NaN and would
 * reject every Ticket Type that has no Purchase Limit at all. Kept pure and
 * dependency-free so it is directly unit-testable, like
 * [promotions.ts](./promotions.ts).
 */

/**
 * What the organizer typed into the Purchase Limit field, resolved. "unset" is
 * a legitimate answer and the common one — it means no Purchase Limit — so it
 * is a distinct outcome from "invalid" rather than being folded into it.
 */
export type ParsedPurchaseLimit =
  | { kind: "unset" }
  | { kind: "invalid" }
  | { kind: "valid"; maxPerCustomer: number };

/**
 * The typed Purchase Limit. Whitespace alone reads as unset: an organizer who
 * clears the field and leaves a stray space means to lift the limit, not to be
 * refused. Anything else must be a whole positive integer — zero would say the
 * Ticket Type may be held by nobody, which is what an unpublished Event or a
 * capacity of zero is for.
 */
export function parsePurchaseLimit(typed: string): ParsedPurchaseLimit {
  const trimmed = typed.trim();
  if (trimmed === "") {
    return { kind: "unset" };
  }
  // Digits only: `Number.parseInt` would happily read "3 tickets" as 3 and
  // "1e9" as 1, silently saving a Purchase Limit the organizer did not type.
  if (!/^\d+$/.test(trimmed)) {
    return { kind: "invalid" };
  }
  const maxPerCustomer = Number.parseInt(trimmed, 10);
  if (!Number.isSafeInteger(maxPerCustomer) || maxPerCustomer < 1) {
    return { kind: "invalid" };
  }
  return { kind: "valid", maxPerCustomer };
}

/**
 * The wire value for a parsed Purchase Limit. Only "unset" and "valid" reach
 * the request: an "invalid" parse is refused on the form before any fetch, so
 * this narrows to the two states the API accepts.
 */
export function purchaseLimitWireValue(
  parsed: { kind: "unset" } | { kind: "valid"; maxPerCustomer: number },
): number | null {
  return parsed.kind === "unset" ? null : parsed.maxPerCustomer;
}

/** The form field's value for a Ticket Type as the API rendered it: "" when unrestricted. */
export function purchaseLimitFormValue(maxPerCustomer: number | null): string {
  return maxPerCustomer === null ? "" : String(maxPerCustomer);
}

/**
 * What the card says about a Purchase Limit. A Ticket Type without one says
 * nothing at all — an "unlimited" line on almost every row would be noise, and
 * the absence of the line is the absence of the limit.
 */
export function purchaseLimitSummary(maxPerCustomer: number | null): string | null {
  if (maxPerCustomer === null) {
    return null;
  }
  return `Purchase Limit: ${maxPerCustomer.toLocaleString()} per customer`;
}

/**
 * The field's hint. It states the one thing an organizer cannot infer from the
 * number: lowering a Purchase Limit is never retroactive (ADR 0025), so nobody
 * expects the tickets already held to be taken back.
 */
export const PURCHASE_LIMIT_HINT =
  "Optional. The most of this ticket type one customer may hold at once. Leave empty for no limit. Applies to future checkouts only — it never cancels tickets already bought.";

/** The refusal, in the same voice as the price and capacity refusals. */
export const PURCHASE_LIMIT_INVALID_MESSAGE =
  "Enter a whole purchase limit of 1 or more, or leave it empty for no limit";
