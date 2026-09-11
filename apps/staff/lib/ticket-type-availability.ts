/**
 * Whether a Ticket Type can be bought right now, as the staff editor reads it.
 *
 * There is no availability field in the catalog: a Ticket Type is buyable when
 * its Event is published, its capacity is not exhausted and its Sales Cutoff
 * has not passed. This derives that one signal so the card states it plainly
 * instead of leaving an organizer to subtract Sold from Capacity in their head
 * and compare a timestamp against the clock. Kept pure and structurally typed
 * so it is directly unit-testable, like [promotions.ts](./promotions.ts).
 */

/**
 * What the card says about buying this Ticket Type at this moment.
 *
 * A token, and only a token. The words for each state live in the message
 * catalogs under `ticketTypes.availabilityNotOnSale` and its three siblings, so
 * that this module stays free of English, of React and of the i18n runtime and
 * its tests keep asserting a decision rather than a sentence (ADR 0041). The
 * badge still carries the whole meaning in words: colour is never the only
 * signal for state (docs/design/foundation.md).
 */
export type Availability = "not_on_sale" | "sold_out" | "closed" | "low_stock" | "on_sale";

/** The capacity pool an Availability is read from. */
export type CapacityPool = {
  capacity: number;
  sold_count: number;
};

/**
 * Everything an Availability is read from: the stock, and the instant the
 * Storefront stops selling it.
 *
 * `sales_cutoff_at` is required and not optional, so a caller cannot forget it
 * and get "never closes" by accident. null is the way to say a Ticket Type
 * never stops selling, which is what most of them say.
 */
export type SellableTicketType = CapacityPool & {
  sales_cutoff_at: string | null;
};

/** At or below this share of capacity remaining, the card warns instead of reassuring. */
const LOW_STOCK_SHARE = 0.1;

/**
 * Whether a Ticket Type whose Sales Cutoff is `salesCutoffAt` has closed by the
 * instant `at` (ADR 0070).
 *
 * The TypeScript twin of the Go predicate `catalog.ClosedAt`, and deliberately
 * shaped like it: half-open with the cutoff instant itself closed, a null
 * cutoff never closing, and the instant a parameter rather than a clock read so
 * the function stays pure. The server is the authority — the staff app only
 * draws what it will decide — so the two must never disagree about an instant,
 * which is what the boundary cases in the test beside this assert.
 *
 * The zone never enters the comparison. Both sides are instants, and the
 * Event's timezone matters only where the value is typed and read back in
 * words.
 *
 * An unparseable value reads as open, which falls out of the comparison rather
 * than being guarded for: every comparison against NaN is false. That is the
 * answer this wants anyway — a shape the app cannot understand is no grounds
 * for telling an organizer their Ticket Type has stopped selling — and the test
 * beside this pins it so the reasoning cannot be lost.
 */
export function closedAt(salesCutoffAt: string | null, at: Date): boolean {
  if (salesCutoffAt === null) {
    return false;
  }
  return at.getTime() >= new Date(salesCutoffAt).getTime();
}

/** What is left of the pool, floored at zero so an over-sold row still reads as sold out. */
export function remainingCapacity(pool: CapacityPool): number {
  return Math.max(0, pool.capacity - pool.sold_count);
}

/**
 * The Ticket Type's availability at an instant.
 *
 * The order of the five states is the Storefront's, and the two apps rank them
 * identically so that a disagreement between them is a bug rather than a matter
 * of taste (ADR 0070). The Event gates first: nothing on a draft or cancelled
 * Event is buyable, however much capacity is left and whatever its cutoff says.
 * Sold out beats closed because it is the stronger fact and it changes what the
 * reader does next — "they are gone" ends the conversation, "we stopped
 * selling" invites an email asking you to reopen. Closed beats low stock
 * because how many are left stops mattering once nobody can buy them.
 *
 * `at` is a parameter and not a clock read, so a card can be rendered at any
 * instant and the decision stays testable.
 */
export function ticketTypeAvailability(
  ticketType: SellableTicketType,
  eventStatus: string,
  at: Date,
): Availability {
  if (eventStatus !== "published") {
    return "not_on_sale";
  }
  const remaining = remainingCapacity(ticketType);
  if (remaining === 0) {
    return "sold_out";
  }
  if (closedAt(ticketType.sales_cutoff_at, at)) {
    return "closed";
  }
  if (ticketType.capacity > 0 && remaining / ticketType.capacity <= LOW_STOCK_SHARE) {
    return "low_stock";
  }
  return "on_sale";
}

/**
 * Badge colour per state, matching the Event status badge idiom.
 *
 * Closed falls to the neutral colour with sold out and not on sale, and
 * deliberately not to the amber the Storefront's countdown wears: the staff
 * card is a control panel, and amber on it would read as a problem to fix
 * rather than as a decision the organizer made on purpose (ADR 0070). Which is
 * again why the badge must also carry a word.
 */
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
 * The three numbers the capacity line is made of: how many are gone, how many
 * there were, and how many are left.
 *
 * NOT a sentence, and no longer formatted here. This module used to build
 * "32 of 100 sold · 68 left" and call `toLocaleString()` on each number, which
 * was wrong twice over once the staff app learned a second language: the wording
 * was English nailed into a pure module, and the marks around the numbers came
 * from the reader's BROWSER rather than from their Staff Locale (ADR 0041). So
 * the parts come out and the catalog says the sentence, with `lib/format.ts`
 * drawing each number in the reader's language — `ticketTypes.capacityLine`.
 */
export type CapacityCounts = {
  /** Never more than `capacity`: an over-sold pool reports a full house. */
  sold: number;
  capacity: number;
  remaining: number;
};

/**
 * The capacity line's parts, never rounded in a way that hides a sold-out pool
 * (docs/design/foundation.md).
 */
export function capacityCounts(pool: CapacityPool): CapacityCounts {
  return {
    sold: Math.min(pool.sold_count, pool.capacity),
    capacity: pool.capacity,
    remaining: remainingCapacity(pool),
  };
}

/** How full the meter reads, 0–1, guarding the capacity-zero case. */
export function soldShare(pool: CapacityPool): number {
  if (pool.capacity <= 0) {
    return 1;
  }
  return Math.min(1, pool.sold_count / pool.capacity);
}
