/**
 * What the terminal pages offer a buyer who has already paid.
 *
 * This is the return leg, and it is the most consequential path in the
 * Storefront for one reason: the money has already moved by the time it runs.
 * Everything else can be tried again; this cannot.
 *
 * SINCE ADR 0054 A BUYER IS SIGNED IN BY CONSTRUCTION, AND THAT SAYS NOTHING
 * ABOUT WHO ARRIVES HERE. Checkout requires a Customer Session, so the person
 * who pressed pay held one — and then left this origin for the Payment Provider,
 * possibly for minutes. A session can end in that gap: a jar cleared by the
 * browser or by hand, a provider webview that keeps its own cookies and drops
 * them, a session revoked from another device, a return in a different browser
 * entirely. The signed-out branch on this leg is therefore NOT guest-checkout
 * residue left over from before the wall. Its rationale changed — from "the
 * buyer never had a session" to "the buyer's session may not have survived the
 * round trip" — and its behaviour must not.
 *
 * The ".ts" in the local imports is written out because these tests run this
 * module directly under `node --experimental-strip-types`, which resolves
 * specifiers exactly. Next resolves it identically (see lib/locale.ts).
 */

import { DEFAULT_DESTINATION, SIGN_IN_PATH } from "./destination.ts";
import { safePrefillEmail } from "./signin-prefill.ts";

/**
 * signInToTicketsHref is the way back in for a buyer who has paid and arrives
 * without a session: sign in with the address the purchase was made under, and
 * land in the Customer Area the tickets are in.
 *
 * The address is a PREFILL and never an assertion — the passcode or the Google
 * round trip still has to be completed, so nothing is granted by putting an
 * address in a field. What it buys is that the buyer lands in the right Customer
 * Area: a purchase made under one address and a session held under another
 * belong to different Customers (ADR 0011), so somebody sent to sign in and
 * reaching for their everyday email would arrive in an Area their new tickets
 * are not in — the exact stranding ADR 0054 exists to end, reintroduced one page
 * later.
 *
 * It comes out of the checkout context cookie, which got it from the API's own
 * report of the address the Ticket Sale was addressed to (#387). It is guarded
 * again here anyway, because a cookie is caller-controlled storage however
 * httpOnly it is and this becomes a query parameter on a sign-in page.
 *
 * Omitted rather than sent blank when this app does not know one — a cookie that
 * expired, was cleared, or belongs to another browser. The field then simply
 * arrives empty, which is a buyer typing their own address rather than a buyer
 * shown a wrong one.
 */
export function signInToTicketsHref(email: string | null | undefined): string {
  const params = new URLSearchParams({ next: DEFAULT_DESTINATION });
  const address = safePrefillEmail(email);
  if (address) {
    params.set("email", address);
  }
  return `${SIGN_IN_PATH}?${params.toString()}`;
}
