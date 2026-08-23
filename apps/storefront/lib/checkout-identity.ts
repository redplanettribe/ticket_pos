/**
 * WHO the purchase will be addressed to, decided from the Customer Session and
 * from nothing else (ADR 0054, #385).
 *
 * This is the predicate the wall at Buy is built on, and it answers one
 * question: is there a proven address to write this Ticket Sale to? A non-null
 * answer means the dialog may open and the session-gated begin-checkout will
 * take it (#384); null means the buyer is sent to sign in first.
 *
 * IT IS THE COURTESY, NOT THE RULE. Enforcement lives in the API, because the
 * BFF is a hop and not a boundary (ADR 0008): the route takes no customer_email
 * at all and reads the address off the session, so a browser that skipped this
 * page entirely cannot address a sale to anybody. What this decides is what a
 * person is shown — a dialog, or a way to sign in.
 *
 * A CONFIRMATION LINK SESSION IS NOT AN IDENTITY HERE. It is minted from a token
 * that travelled inside a receipt and may have been forwarded, so it is not
 * Proof of Email Ownership; the API refuses a checkout on one with 403
 * CUSTOMER_SESSION_SCOPE_INSUFFICIENT, and drawing the dialog for its holder
 * would collect a whole form on the way to that refusal. They get the wall, and
 * signing in properly is exactly what widens them.
 *
 * The prefill rides along rather than being fetched when the dialog opens, and
 * that is the point of gathering it here: who is buying, what may still be asked
 * of them, and what their stored details are come from ONE snapshot of ONE
 * session and cannot disagree. It also means the address is on screen the
 * instant the dialog is, with no moment where the dialog is open and cannot say
 * whose purchase this is.
 */

// The ".ts" is written out because the unit tests run this module directly under
// `node --experimental-strip-types`, which resolves specifiers exactly. Next
// resolves it identically.
import type { ConsentBoxes } from "./checkout-consent.ts";

/**
 * The facts this decision needs off a Customer Session, and no others.
 *
 * Written structurally rather than by importing CustomerSession, so this module
 * stays pure and runnable under `node --experimental-strip-types`: the real type
 * has everything below and more, and satisfies this by shape.
 */
export type CheckoutSessionFacts = {
  email: string;
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
  phone: string | null;
  /** Set on a Confirmation Link session, and null on a full Customer Session. */
  ticket_sale_id: string | null;
  consent_boxes: ConsentBoxes;
};

/** The three outcomes of a session read, as lib/customer-session.ts reports them. */
export type CheckoutSessionRead =
  | { status: "ok"; data: CheckoutSessionFacts }
  | { status: "signed-out" }
  | { status: "error"; code: string | null; message: string | null };

/**
 * Who is buying, as the dialog needs it.
 *
 * `email` is the loud part: the dialog states it prominently rather than as fine
 * print, because a Customer Session satisfies the wall for its whole life with
 * no re-proof at the till — the Customer Area already exposes strictly more
 * behind the same session — and on a shared machine the mitigation is that the
 * identity is impossible to miss, not that it is checked again.
 *
 * The rest is prefill. `firstName` and `lastName` are routinely EMPTY and that
 * is not a defect: signing in mints a Customer with no name, because the sign-in
 * form asks only for an address, a code and consent. The dialog collects them,
 * which is why it still has those two fields.
 */
export type CheckoutIdentity = {
  email: string;
  firstName: string;
  lastName: string;
  /**
   * The stored Tax ID, both halves or neither. A half of one prefills nothing:
   * the form's two controls are one fact, and seeding a number under a type
   * nobody stored — or a type over an empty number — reads as this app knowing
   * something it does not (ADR 0016).
   */
  taxIdType: string | null;
  taxIdNumber: string | null;
  /** The stored phone in canonical E.164, null when the Customer has none (#108). */
  phone: string | null;
  /**
   * Which boxes this Customer still owes an answer to (#254). Ordinarily NONE
   * by the time anybody reaches the dialog: a first-time buyer meets them at
   * sign-in, which holds the sign-in at a consent step when Policy Acceptance is
   * outstanding. Consent is asked once, in one place, and this is what makes the
   * dialog draw a second copy of it exactly never.
   */
  consentBoxes: ConsentBoxes;
};

/**
 * checkoutIdentity is the whole wall in one function: an identity, or null.
 *
 * Null for a visitor with no session, for a Confirmation Link session, and for a
 * session read that FAILED — the last of those on purpose. An unreachable API
 * leaves this app unable to say whose purchase it would be, and a dialog that
 * cannot name the address it is writing to must not collect a purchase; the
 * buyer meets the sign-in page instead, which reads the same session and lets
 * them straight through if one turns out to be there after all. No loop, and no
 * sale addressed to a guess.
 */
export function checkoutIdentity(read: CheckoutSessionRead): CheckoutIdentity | null {
  if (read.status !== "ok" || read.data.ticket_sale_id !== null) {
    return null;
  }
  const email = read.data.email.trim();
  // A session with no address is not a thing the API produces; treated as no
  // identity rather than defended against downstream, because everything this
  // returns is drawn beside that address and a blank one would be an empty
  // statement about who is buying.
  if (email === "") {
    return null;
  }
  const taxIdType = read.data.tax_id_type?.trim() || null;
  const taxIdNumber = read.data.tax_id_number?.trim() || null;
  const bothHalves = taxIdType !== null && taxIdNumber !== null;
  return {
    email,
    firstName: read.data.first_name.trim(),
    lastName: read.data.last_name.trim(),
    taxIdType: bothHalves ? taxIdType : null,
    taxIdNumber: bothHalves ? taxIdNumber : null,
    phone: read.data.phone?.trim() || null,
    consentBoxes: read.data.consent_boxes,
  };
}
