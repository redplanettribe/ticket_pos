/**
 * Which consent boxes the checkout dialog draws, and for whom (#254, parent
 * #249).
 *
 * THE RULE IS ONE SENTENCE: a buyer is shown every box unless the API has told
 * them, about themselves, that they have already answered some. Everything
 * below is that sentence and its two ways of not applying.
 *
 * The set comes from the Customer Session read — `consent_boxes`, published
 * beside the email and Tax ID the dialog prefills from — so the answers to "who
 * is buying" and "what may still be asked of them" come from ONE snapshot of ONE
 * session and cannot disagree. Nothing here computes the set: which boxes a
 * Customer owes is the platform's finding (a Policy Version bump, a Pending
 * Confirmation) and this app only reads it.
 *
 * It decides nothing either. The API recomputes the same set at begin-checkout
 * and drops answers for boxes that were not owed, so a wrong guess here shows a
 * buyer a box too many or too few — never a purchase refused, and never a
 * standing answer churned.
 */

/** The three boxes, in the API's own vocabulary and shape. */
export type ConsentBoxes = {
  policy_acceptance: boolean;
  marketing_consent: boolean;
  networking_consent: boolean;
};

/**
 * What a guest is shown: everything. Nothing is known about a visitor with no
 * session, and the address they typed is a claim — so the Short Notice and all
 * three boxes appear, exactly as they did before this ticket (#253).
 */
export const EVERY_CONSENT_BOX: ConsentBoxes = {
  policy_acceptance: true,
  marketing_consent: true,
  networking_consent: true,
};

/** The session facts this decision needs, and no others. */
export type ConsentSession = {
  /** The address the session belongs to, as the API reports it. */
  email: string;
  /** Which boxes that Customer still owes an answer to. */
  consent_boxes: ConsentBoxes;
};

/**
 * The boxes to draw for this dialog, given whatever session read came back and
 * whatever address is in the form right now.
 *
 * `session` is null for an anonymous visitor AND for a session read that failed:
 * a checkout must never break over this, and the failure mode is the safe one —
 * every box, which the API accepts from anybody.
 *
 * THE EMAIL FIELD IS PART OF THE PREDICATE, which is the non-obvious half. A
 * signed-in Customer who types a friend's address into the dialog is supplying
 * the friend's details, and the sale, the Customer record and the Consent Record
 * will all be the friend's — under an address this session proves nothing about.
 * The API treats that checkout as a guest's (selfAssertedCheckout) and owes it
 * every box, so the dialog must draw every box the moment the two diverge, or it
 * would hide a required box and hand the buyer a refusal at the pay button.
 */
export function checkoutConsentBoxes(
  session: ConsentSession | null,
  formEmail: string,
): ConsentBoxes {
  if (!session) return EVERY_CONSENT_BOX;
  if (normalizeEmail(formEmail) !== normalizeEmail(session.email)) return EVERY_CONSENT_BOX;
  return session.consent_boxes;
}

/**
 * Whether any consent UI is drawn at all. A Customer who owes nothing sees no
 * Short Notice, no boxes and no link: the checkout is exactly what it was before
 * this feature existed (parent #249, user story 10).
 */
export function anyConsentBox(boxes: ConsentBoxes): boolean {
  return boxes.policy_acceptance || boxes.marketing_consent || boxes.networking_consent;
}

/**
 * Case and surrounding space are not part of an address, and the API compares
 * the two the same way (platform.NormalizeEmail). Local-part case is technically
 * significant to some mail servers and ignored here for the same reason it is
 * ignored there: it is a comparison this platform makes about its own Customers.
 */
function normalizeEmail(email: string): string {
  return email.trim().toLowerCase();
}
