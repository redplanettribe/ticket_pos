/**
 * A Ticket Type's Promotion as the staff editor reasons about it (ADR 0021):
 * one time-boxed Promotional Price that overrides the List Price while its
 * window holds. Kept pure and dependency-free — no React, no i18n runtime, no
 * catalog — so the state derivation is directly unit-testable under the fast
 * runner. Every sentence this module once carried now lives in the catalogs.
 */

/** The Ticket Type's one Promotion slot, exactly as the staff API renders it. */
export type Promotion = {
  promotional_price_cents: number;
  /** null means the Promotion was live from the moment it was saved. */
  starts_at: string | null;
  ends_at: string;
};

/**
 * Where a Promotion sits relative to an instant: before, inside, or past its
 * window.
 *
 * A token. The badge's words for each state are `ticketTypes.promotionScheduled`
 * and its two siblings in the message catalogs (ADR 0041).
 */
export type PromotionState = "scheduled" | "live" | "ended";

/**
 * The Promotion's state at an instant. Mirrors the Go window predicate
 * (`catalog.Promotion.LiveAt`): half-open, start inclusive, end exclusive, and
 * an absent start means live from when it was saved.
 */
export function promotionState(promotion: Promotion, at: Date): PromotionState {
  const now = at.getTime();
  const endsAt = new Date(promotion.ends_at).getTime();
  if (now >= endsAt) {
    return "ended";
  }
  if (promotion.starts_at !== null && now < new Date(promotion.starts_at).getTime()) {
    return "scheduled";
  }
  return "live";
}

/** Badge colour for a Promotion state, matching the Event status badge idiom. */
export function promotionStateBadgeVariant(state: PromotionState): "success" | "warning" | "secondary" {
  switch (state) {
    case "live":
      return "success";
    case "scheduled":
      return "warning";
    default:
      return "secondary";
  }
}

/**
 * The API error codes that are about the Promotion itself.
 *
 * This module used to hold a sentence for each. It holds the codes only now:
 * the words are `errors.envelope` in both message catalogs, resolved by
 * `lib/api-errors.ts` the way every other staff failure is (ADR 0023, ADR 0041),
 * so there is one place a failure is worded rather than two.
 *
 * What is left here is a decision and not copy — WHICH refusals belong beside
 * the price field rather than in a toast that scrolls away. A List Price edit
 * rejected because it would sink under a live Promotional Price has to be said
 * where the price the organizer just typed still is; a load failure does not.
 */
export const PROMOTION_ERROR_CODES = [
  "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE",
  "PROMOTION_ALREADY_EXISTS",
  "PROMOTION_NOT_FOUND",
  "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE",
] as const;

export type PromotionErrorCode = (typeof PROMOTION_ERROR_CODES)[number];

/** Whether a refusal is a Promotion's, and so belongs inline on the form. */
export function isPromotionErrorCode(code: string | null | undefined): code is PromotionErrorCode {
  return typeof code === "string" && (PROMOTION_ERROR_CODES as readonly string[]).includes(code);
}
