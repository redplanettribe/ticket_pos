/**
 * A Ticket Type's Promotion as the staff editor reasons about it (ADR 0021):
 * one time-boxed Promotional Price that overrides the List Price while its
 * window holds. Kept pure and dependency-free so the state derivation and the
 * error copy are directly unit-testable.
 */

/** The Ticket Type's one Promotion slot, exactly as the staff API renders it. */
export type Promotion = {
  promotional_price_cents: number;
  /** null means the Promotion was live from the moment it was saved. */
  starts_at: string | null;
  ends_at: string;
};

/** Where a Promotion sits relative to an instant: before, inside, or past its window. */
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

export const PROMOTION_STATE_LABELS: Record<PromotionState, string> = {
  scheduled: "Scheduled",
  live: "Live",
  ended: "Ended",
};

/**
 * Staff-facing copy for the Promotion API error codes, so an organizer reads
 * what to do rather than the raw code. Anything unmapped falls back to the
 * server's own message.
 */
const PROMOTION_ERROR_MESSAGES: Record<string, string> = {
  PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE:
    "The promotional price must be below the list price. Lower it, or raise the list price first.",
  PROMOTION_ALREADY_EXISTS:
    "This ticket type already has a promotion. Reload the section to edit the existing one.",
  PROMOTION_NOT_FOUND: "This ticket type no longer has a promotion. Reload the section and set a new one.",
  LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE:
    "The list price must stay above the promotional price. Adjust or remove the promotion first.",
};

/** The inline message for an API error code, or null to fall back to the server message. */
export function promotionErrorMessage(code: string | undefined): string | null {
  if (!code) {
    return null;
  }
  return PROMOTION_ERROR_MESSAGES[code] ?? null;
}
