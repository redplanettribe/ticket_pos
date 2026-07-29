/**
 * What any Storefront surface may say about undoing a purchase, and where it
 * sends somebody who wants to.
 *
 * Three surfaces now say it: the Customer Area's cards, the checkout success
 * page a guest lands on seconds after paying, and the Confirmation Link page
 * they come back to when they reconsider (#121). Only the first of the three can
 * actually undo anything — reversal requires a Customer Session, because a
 * Confirmation Link travels by email and gets forwarded (ADR 0018) — so the
 * other two state the deadline and offer the way in.
 *
 * The word is "undo" everywhere. Not "cancel", which in this system means an
 * Event being called off, and not "refund", which is wrong for a free Online
 * Sale where no money ever moved.
 *
 * This module holds the decisions the three share, so a change of copy or of
 * destination cannot reach one page and miss another.
 */

import { customerAreaSaleHref } from "./destination.ts";
import { DEFAULT_LOCALE, formatReversalDeadline, type IntlLocale } from "./format.ts";

/**
 * What the API says about undoing one Ticket Sale. The Customer Area's cards and
 * the guest checkout read return these fields, computed by one rule on the API
 * side — which is what makes the deadline drawn from either of them the same
 * instant for the same sale.
 *
 * `reversible_until` is the deadline on the offer above and not a publication of
 * the Reversal Window: a sale whose payment cannot be reversed has an open
 * Window and no offer, and reports null here.
 *
 * `ticket_sale_id` is present only on the guest checkout read, where it is the
 * one way that page learns which sale it is talking about — a Customer Area card
 * already knows, from its own `id`.
 */
export type ReversalOffer = {
  reversible: boolean;
  reversible_until: string | null;
  ticket_sale_id?: string | null;
};

/**
 * undoDeadline is the deadline to draw, or null when this page must say nothing
 * about undoing at all.
 *
 * Null covers every "no": the sale was never eligible, its window has closed,
 * the payment cannot be reversed, the API could not be reached. A page that gets
 * null draws no greyed-out button, no expired countdown and no explanation —
 * there is nothing on offer, so there is nothing to explain.
 *
 * The two fields are trusted rather than re-derived. Whether a purchase can be
 * undone depends on the Reversal Window, the Sales Channel and whether the
 * payment can be reversed at all, and every one of those is re-checked by the API
 * when the undo is actually pressed.
 *
 * The label is drawn in Ecuador time, because the 20:00 cutoff is an Ecuadorian
 * wall-clock rule (ADR 0018). Rendering it in the Event's timezone — the
 * obvious-looking thing to do on a page about an Event — would print an hour
 * matching no rule anybody stated.
 */
export function undoDeadline(
  offer: ReversalOffer | null | undefined,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  if (!offer?.reversible || !offer.reversible_until) {
    return null;
  }
  return formatReversalDeadline(offer.reversible_until, locale);
}

/**
 * signInToUndoHref is the one click that turns the session requirement from a
 * dead end into a door: sign in with the address the purchase was made under,
 * and land on that purchase, where the undo lives.
 *
 * The destination is the sale itself and not merely the Customer Area (#121).
 * Someone arriving here has one purchase in mind — usually one they made
 * minutes ago — and a buyer with a season's worth of tickets would otherwise be
 * handed a list to scan at the exact moment they are anxious about their money.
 * When this app does not know which sale it means, the list is the destination,
 * which is what it always was.
 *
 * The email is a prefill and never an assertion — the passcode still has to be
 * proved, so nothing is granted by putting an address in a field. It is omitted
 * entirely when this app does not know one, rather than sent blank, so a visitor
 * whose checkout context has expired gets an empty field instead of a wrong one.
 *
 * A purchase made under one address and a session held under another are
 * different Customers (ADR 0011), which is exactly why the address travels: a
 * buyer signed into their everyday email would otherwise be sent to a Customer
 * Area their purchase is not in.
 */
export function signInToUndoHref(
  email: string | null | undefined,
  ticketSaleId: string | null | undefined,
): string {
  const params = new URLSearchParams({ next: customerAreaSaleHref(ticketSaleId) });
  const address = email?.trim();
  if (address) {
    params.set("email", address);
  }
  return `/signin?${params.toString()}`;
}
