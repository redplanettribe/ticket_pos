/**
 * Which consent boxes the checkout dialog draws (#254, parent #249; contracted
 * by ADR 0054, #385).
 *
 * THE RULE IS NOW ONE SENTENCE WITH NO EXCEPTIONS: the dialog draws the boxes
 * the Customer on the session still owes, and nothing else.
 *
 * The set comes from the Customer Session read, alongside the address the
 * purchase is being written to and the details it prefills from
 * (lib/checkout-identity.ts) — so "who is buying" and "what may still be asked
 * of them" come from ONE snapshot of ONE session and cannot disagree. Nothing
 * here computes the set: which boxes a Customer owes is the platform's finding
 * (a Policy Version bump, a Pending Confirmation) and this app only reads it.
 * The API recomputes the same set at begin-checkout and drops answers for boxes
 * that were not owed, so a wrong guess here shows a buyer a box too many or too
 * few — never a purchase refused, and never a standing answer churned.
 *
 * ORDINARILY IT DRAWS NOTHING AT ALL. Checkout begins signed in, and a
 * first-time buyer meets the boxes at SIGN-IN — which holds the sign-in at a
 * consent step when Policy Acceptance is outstanding (#251). By the time anybody
 * reaches the dialog there is nothing outstanding to ask. Consent is asked once,
 * in one place, and the branch below is what keeps a Policy Version published
 * mid-session from slipping past.
 *
 * The email field this module used to reason about is gone with ADR 0054, and so
 * is the rule that went with it: a signed-in Customer typing a friend's address
 * was a guest checkout for somebody else and was owed every box again. There is
 * no address to type any more, so there is no divergence to detect. The whole
 * "every box" fallback went with it — nobody without an identity ever reaches
 * this dialog.
 */

/** The three boxes, in the API's own vocabulary and shape. */
export type ConsentBoxes = {
  policy_acceptance: boolean;
  marketing_consent: boolean;
  networking_consent: boolean;
};

/**
 * Whether any consent UI is drawn at all. A Customer who owes nothing sees no
 * Short Notice, no boxes and no link: the checkout is exactly what it was before
 * this feature existed (parent #249, user story 10), which since #385 is the
 * ordinary case rather than the reward for a returning buyer.
 */
export function anyConsentBox(boxes: ConsentBoxes): boolean {
  return boxes.policy_acceptance || boxes.marketing_consent || boxes.networking_consent;
}
