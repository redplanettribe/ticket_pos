/**
 * Where a visitor is sent inside this Storefront, and how that destination is
 * spelled.
 *
 * The guard lives on its own because two unrelated paths decide the post-sign-in
 * destination — the sign-in page reading `?next=`, and the Google Sign-In
 * callback reading it back out of its state cookie — and both must apply it. One
 * copy means a fix cannot reach one path and miss the other.
 *
 * The Customer Area's per-sale address lives here for the same reason: the card
 * that carries the anchor and the several links that aim at it are in different
 * files, and a link to an id nothing renders is a link that silently does
 * nothing.
 */

/** Where a Customer lands when nothing better was asked for: their Customer Area. */
export const DEFAULT_DESTINATION = "/tickets";

/**
 * ticketSaleAnchorId is the DOM id one Ticket Sale's card carries in the Customer
 * Area, and the only place that spelling is decided.
 *
 * The Customer Area is one page listing every purchase, with no route of its own
 * per sale. A buyer arriving to undo the thing they bought ninety seconds ago
 * should land on it rather than on a list to scan, and a fragment is the honest
 * way to say that without inventing a route: the browser scrolls to it, and a
 * page that has changed underneath simply shows the list, which is where they
 * were going anyway.
 *
 * Prefixed rather than raw so it cannot collide with another id on the page, and
 * so an element with this id is recognisably a sale.
 */
export function ticketSaleAnchorId(ticketSaleId: string): string {
  return `sale-${ticketSaleId}`;
}

/**
 * customerAreaSaleHref points at one purchase in the Customer Area, falling back
 * to the Area itself when the caller does not know which sale it means.
 *
 * That fallback is the ordinary case rather than an error: a guest whose checkout
 * context cookie has expired, or a purchase with no undo on offer, gives this app
 * no sale id, and the whole list is a correct destination — just a less precise
 * one. Nothing here asserts ownership; the Customer Area serves what the session
 * owns and nothing else, so a fragment naming a sale that is not theirs scrolls
 * nowhere.
 */
export function customerAreaSaleHref(ticketSaleId: string | null | undefined): string {
  const id = ticketSaleId?.trim();
  return id ? `${DEFAULT_DESTINATION}#${ticketSaleAnchorId(id)}` : DEFAULT_DESTINATION;
}

/**
 * safeNext keeps the post-sign-in destination inside this Storefront. Anything
 * that is not a plain absolute path — a full URL, or a protocol-relative "//host"
 * — is discarded, so the sign-in page can never be used to bounce a visitor to
 * another site.
 */
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) {
    return DEFAULT_DESTINATION;
  }
  return next;
}
