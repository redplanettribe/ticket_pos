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

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import { pathWithQuery } from "./current-path.ts";

/** Where a Customer lands when nothing better was asked for: their Customer Area. */
export const DEFAULT_DESTINATION = "/tickets";

/** The sign-in page's own address, spelled once. */
export const SIGN_IN_PATH = "/signin";

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
 *
 * A query string is deliberately allowed through, and on some pages it is the
 * only thing that makes the destination meaningful: see signInHref.
 */
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) {
    return DEFAULT_DESTINATION;
  }
  return next;
}

/**
 * signInHref is the header's way in, carrying where the visitor already is so
 * that proving who they are does not cost them their place.
 *
 * The whole address travels, query included. That is the fix for a bug worth
 * naming, because "the current page" looked like it meant the path: a buyer who
 * had just paid and pressed Sign in was sent back to /checkout/success with the
 * `ref` stripped off — and the Sale Confirmation reference lives in that query
 * and in no cookie, session or API this app could ask. Their confirmation was
 * simply gone. The explorer loses less but loses it the same way: search terms
 * and filters are query too, and signing in emptied them.
 *
 * Two destinations are refused rather than carried. "/tickets" is where sign-in
 * goes anyway, so naming it adds nothing, and "/signin" would point the page at
 * itself — a visitor who reached the form, wandered, and came back would be
 * handed a `next` that lands them back on the form they just completed.
 *
 * The path is locale-free on the way through: it comes from next-intl's
 * `usePathname` with the prefix already off, and gets one back from `Link`. So a
 * `next` written here means the same page in whichever language the visitor
 * finishes signing in under, which is what lets someone switch language mid
 * sign-in and still land where they were going.
 */
export function signInHref(pathname: string, search?: string | null): string {
  const target = pathWithQuery(pathname, search);
  // Compared on the path alone: "/tickets?page=2" is still the Customer Area,
  // and a check against the whole address would let a query smuggle a pointless
  // round trip back in.
  const path = target.split("?")[0];
  if (path === DEFAULT_DESTINATION || path.startsWith("/signin")) {
    return SIGN_IN_PATH;
  }
  return `${SIGN_IN_PATH}?next=${encodeURIComponent(target)}`;
}
