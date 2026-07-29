/**
 * Whether a Ticket Type can be bought right now, as the staff editor reads it.
 *
 * There is no availability field in the catalog: a Ticket Type is buyable when
 * its Event is published and its capacity is not exhausted. This derives that
 * one signal so the card states it plainly instead of leaving an organizer to
 * subtract Sold from Capacity in their head. Kept pure and structurally typed
 * so it is directly unit-testable, like [promotions.ts](./promotions.ts).
 */

/** What the card says about buying this Ticket Type at this moment. */
export type Availability = "not_on_sale" | "sold_out" | "low_stock" | "on_sale";

/** The capacity pool an Availability is read from. */
export type CapacityPool = {
  capacity: number;
  sold_count: number;
};

/** At or below this share of capacity remaining, the card warns instead of reassuring. */
const LOW_STOCK_SHARE = 0.1;

/** What is left of the pool, floored at zero so an over-sold row still reads as sold out. */
export function remainingCapacity(pool: CapacityPool): number {
  return Math.max(0, pool.capacity - pool.sold_count);
}

/**
 * The Ticket Type's availability. The Event gates it first: nothing on a draft
 * or cancelled Event is buyable, however much capacity is left.
 */
export function ticketTypeAvailability(pool: CapacityPool, eventStatus: string): Availability {
  if (eventStatus !== "published") {
    return "not_on_sale";
  }
  const remaining = remainingCapacity(pool);
  if (remaining === 0) {
    return "sold_out";
  }
  if (pool.capacity > 0 && remaining / pool.capacity <= LOW_STOCK_SHARE) {
    return "low_stock";
  }
  return "on_sale";
}

/** Badge colour per state, matching the Event status badge idiom. */
export function availabilityBadgeVariant(
  availability: Availability,
): "success" | "warning" | "secondary" {
  switch (availability) {
    case "on_sale":
      return "success";
    case "low_stock":
      return "warning";
    default:
      return "secondary";
  }
}

/**
 * The badge label. It carries the whole meaning on its own: colour is never the
 * only signal for state (docs/design/foundation.md).
 */
export const AVAILABILITY_LABELS: Record<Availability, string> = {
  not_on_sale: "Not on sale",
  sold_out: "Sold out",
  low_stock: "Low stock",
  on_sale: "On sale",
};

/**
 * The capacity line, with thousands separators and never rounded in a way that
 * hides a sold-out pool (docs/design/foundation.md).
 */
export function capacitySummary(pool: CapacityPool): string {
  const sold = Math.min(pool.sold_count, pool.capacity).toLocaleString();
  return `${sold} of ${pool.capacity.toLocaleString()} sold · ${remainingCapacity(pool).toLocaleString()} left`;
}

/** How full the meter reads, 0–1, guarding the capacity-zero case. */
export function soldShare(pool: CapacityPool): number {
  if (pool.capacity <= 0) {
    return 1;
  }
  return Math.min(1, pool.sold_count / pool.capacity);
}
