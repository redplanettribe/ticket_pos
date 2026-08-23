/**
 * The wall at Buy: where pressing it signed out leads, and how the buyer gets
 * back to exactly what they were doing (ADR 0054, #385).
 *
 * Checkout begins signed in, and the address a Ticket Sale is written to comes
 * from a Customer Session rather than from a field (#384). So the Buy button has
 * two behaviours and this module spells the second one: for a visitor with no
 * session it is a REDIRECT rather than a dialog.
 *
 * THE WALL STANDS AT BUY AND NOWHERE EARLIER. The Organization page, the Event
 * page, its Ticket Types, its prices and its steppers stay anonymous, because
 * the Storefront is a discovery surface and gating discovery is the one thing
 * ADR 0002 and ADR 0037 forbid. Buy is also the last honest moment to ask: it
 * crosses a page boundary this app controls, so the bad news reaches the buyer
 * before they have typed a character, rather than after they have filled a form
 * in.
 *
 * Everything the trip has to survive travels in the open, as ONE ADDRESS:
 *
 *     /signin?next=/{org}/events/{event}?sel=<basket>&checkout=1
 *
 * `next` is the mechanism the sign-in page already reads and that Google
 * Sign-In already carries across its callback in the state cookie, beside the
 * Follow intent it carries for the same reason (lib/follow-intent.ts). `sel` is
 * the basket (lib/selection-url.ts). `checkout=1` is the one thing added here:
 * a marker saying the buyer was mid-purchase, so the Event page they land back
 * on opens the dialog instead of making them press Buy a second time.
 *
 * NO NEW COOKIE, NO NEW EXPIRY POLICY, AND NOTHING GRANTED. Every part of this
 * address is forgeable and none of it can spell an identity, a price or a
 * permission: `sel` is re-judged on arrival against live availability and the
 * Purchase Limit, and `checkout=1` opens a dialog that a signed-out visitor
 * cannot reach at all. Somebody who hand-types the whole thing gets the page
 * they would have got by pressing two buttons.
 */

// The ".ts" is written out because the unit tests run this module directly under
// `node --experimental-strip-types`, which resolves specifiers exactly. Next
// resolves it identically.
import { SIGN_IN_PATH, safeNext } from "./destination.ts";
import { SELECTION_PARAM, encodeSelection } from "./selection-url.ts";

/**
 * The marker that says "this buyer was on their way to pay". The only place
 * that spelling is decided, read by the Event page and by the sign-in page.
 */
export const OPEN_CHECKOUT_PARAM = "checkout";

/** Its one accepted value. Anything else is not this marker. */
const OPEN_CHECKOUT_VALUE = "1";

/**
 * Every character encodeSelection can emit, and the guard that the raw query
 * below is allowed to skip escaping.
 *
 * The selection is written into the address UNESCAPED so that `sel=ga:2,vip:1`
 * stays a string a person can read in a log and diff by eye — which was the
 * whole argument for putting the basket in the URL rather than in storage
 * (#383). Every one of these characters is legal unescaped in a query, and
 * encodeSelection cannot produce another; this asserts that rather than assumes
 * it, and a string that somehow failed is left off the address entirely. The
 * safe direction: a buyer whose basket did not travel presses two more buttons,
 * where a mis-escaped one would arrive as junk and be silently dropped anyway.
 */
const SPELLABLE_SELECTION = /^[A-Za-z0-9_,:-]+$/;

/**
 * Where the buyer comes back to: their own Event page, their basket restored,
 * and the checkout dialog open.
 *
 * `eventPath` is locale-free — it comes from next-intl's `usePathname` with the
 * prefix already off, and gets one back from the router — which is what lets
 * somebody switch language mid sign-in and still land on the page they were
 * buying from. Any query or fragment already on it is dropped rather than
 * merged: what this address means is "this Event, this basket, this dialog",
 * and the one parameter worth keeping is `ref`, whose Affiliate click was
 * already banked in a cookie on the way in (ADR 0022) and would only be counted
 * twice by coming back through it.
 */
export function checkoutReturnPath(
  eventPath: string,
  selection: Record<string, number>,
): string {
  // Guarded even though the caller is a pathname: this string becomes a `next`,
  // and `next` is the one thing on the sign-in page that decides where somebody
  // is sent. One guard, applied everywhere, is cheaper than an argument about
  // which callers are trustworthy.
  const path = safeNext(eventPath).split("#")[0].split("?")[0];
  const encoded = encodeSelection(selection);
  // An empty basket is written as NO PARAMETER rather than as `sel=`, which is
  // what lib/selection-url.ts asks of its producers: an empty string decodes to
  // an empty selection either way, and the shorter address is the honest one.
  const carriesSelection = encoded !== "" && SPELLABLE_SELECTION.test(encoded);
  const query = carriesSelection
    ? `${SELECTION_PARAM}=${encoded}&${OPEN_CHECKOUT_PARAM}=${OPEN_CHECKOUT_VALUE}`
    : `${OPEN_CHECKOUT_PARAM}=${OPEN_CHECKOUT_VALUE}`;
  return `${path}?${query}`;
}

/**
 * Where "Try again" goes after a Payment that did not become a sale: the Event
 * page, with the basket the buyer was about to pay for.
 *
 * The selection arrives ALREADY ENCODED, out of the checkout context cookie the
 * begin-checkout hop wrote — this is the last moment anything on this origin
 * knew the quantities, because the buyer left for the Payment Provider and the
 * page state that held them is gone.
 *
 * Deliberately NOT `checkout=1`. A declined Payment is bad news, and a dialog
 * that reopened by itself on top of it would be this app pressing Buy on the
 * buyer's behalf immediately after their card was refused. They get their
 * basket back and press it themselves.
 */
export function retryCheckoutPath(eventPath: string, encodedSelection: string): string {
  const path = safeNext(eventPath).split("#")[0].split("?")[0];
  const encoded = encodedSelection.trim();
  return encoded !== "" && SPELLABLE_SELECTION.test(encoded)
    ? `${path}?${SELECTION_PARAM}=${encoded}`
    : path;
}

/**
 * checkoutSignInHref is the wall itself: where the Buy button points for a
 * visitor with no Customer Session.
 *
 * It is the ordinary sign-in page and not a checkout-flavoured copy of it. Both
 * doors work, both mint the same Customer Session, and a first-time buyer meets
 * the consent step there — which is the whole reason the dialog needs no consent
 * branching of its own (#254, ADR 0035).
 *
 * WHY the buyer is being asked is not a parameter. The sign-in page reads it off
 * `next` (isCheckoutDestination below), so the sentence cannot drift from the
 * destination and survives every detour that carries `next` — a Google Sign-In
 * that failed, and one that was held at consent — without either of those paths
 * having to learn about checkout.
 */
export function checkoutSignInHref(
  eventPath: string,
  selection: Record<string, number>,
): string {
  const next = checkoutReturnPath(eventPath, selection);
  return `${SIGN_IN_PATH}?next=${encodeURIComponent(next)}`;
}

/**
 * Whether an arriving Event page address asks for the checkout dialog.
 *
 * Exactly the one value, and never a repeated parameter: Next hands a repeated
 * one back as an array, and two answers to a yes/no question is no answer.
 *
 * It is a REQUEST AND NOT A GRANT. The page still decides whether there is
 * anybody to sell to and anything in the basket to sell; this only reports what
 * the address asked for.
 */
export function opensCheckout(raw: string | string[] | null | undefined): boolean {
  return raw === OPEN_CHECKOUT_VALUE;
}

/**
 * Whether a post-sign-in destination belongs to a buyer who was on their way to
 * pay — which is what lets the sign-in page say WHY it is asking.
 *
 * Read off the destination rather than passed beside it, deliberately. There is
 * then no way to reach the form carrying a checkout `next` and a reason that
 * says something else, and no third path to remember to update.
 */
export function isCheckoutDestination(next: string | null | undefined): boolean {
  const target = next?.split("#")[0] ?? "";
  const query = target.indexOf("?");
  if (query < 0) {
    return false;
  }
  return new URLSearchParams(target.slice(query + 1)).get(OPEN_CHECKOUT_PARAM) === OPEN_CHECKOUT_VALUE;
}
