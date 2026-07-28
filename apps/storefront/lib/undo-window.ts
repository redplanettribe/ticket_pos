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

import { DEFAULT_DESTINATION } from "./destination.ts";
import { formatReversalDeadline } from "./format.ts";

/**
 * What the API says about undoing one Ticket Sale. The Customer Area's cards and
 * the guest checkout read return exactly these two fields, computed by one rule
 * on the API side — which is what makes the deadline drawn from either of them
 * the same instant for the same sale.
 */
export type ReversalOffer = {
  reversible: boolean;
  reversal_window_closes_at: string | null;
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
export function undoDeadline(offer: ReversalOffer | null | undefined): string | null {
  if (!offer?.reversible || !offer.reversal_window_closes_at) {
    return null;
  }
  return formatReversalDeadline(offer.reversal_window_closes_at);
}

/**
 * signInToUndoHref is the one click that turns the session requirement from a
 * dead end into a door: sign in with the address the purchase was made under,
 * and land in the Customer Area, where the undo lives.
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
export function signInToUndoHref(email: string | null | undefined): string {
  const params = new URLSearchParams({ next: DEFAULT_DESTINATION });
  const address = email?.trim();
  if (address) {
    params.set("email", address);
  }
  return `/signin?${params.toString()}`;
}

/**
 * safePrefillEmail guards what reaches the sign-in field from a query string.
 *
 * It is caller-controlled text on a page anybody can link to, and its only job
 * is to save a buyer from typing their own address. Anything that is not
 * plausibly one is dropped: a blank field is a mild inconvenience, while an
 * arbitrary string sitting in an input on a sign-in page reads as something this
 * app is asserting about the visitor.
 */
export function safePrefillEmail(email: string | null | undefined): string {
  const address = email?.trim() ?? "";
  if (address.length === 0 || address.length > 254) {
    return "";
  }
  // Deliberately loose: the API decides what an email is, and the passcode
  // decides who owns it. This only refuses what obviously is not one.
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address) ? address : "";
}
