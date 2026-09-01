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

/**
 * The four boxes, in the API's own vocabulary and shape. The Terms box joined
 * in #537 (ADR 0066): the other REQUIRED box, owed independently of the
 * policy's because the two documents version independently — and, like every
 * box here, ordinarily false because it was settled at sign-in (#536). It is
 * drawn here for exactly one person: a Customer whose live session spans a
 * Terms edition — the one-time re-gate, or a later bump — for whom this
 * dialog is the backstop that lets no Online Sale complete unaccepted.
 *
 * The Adulthood Declaration joined it in #588 (ADR 0069), and it RIDES the box
 * above rather than being owed on its own account: it is true only where
 * `terms_acceptance` is, and only where the Terms edition in effect also
 * publishes the `label-adulthood-declaration` Artifact to word it with. There is
 * no age gate here and no standing to compute — under an edition that does not
 * ask, it is simply always false.
 */
export type ConsentBoxes = {
  policy_acceptance: boolean;
  marketing_consent: boolean;
  networking_consent: boolean;
  terms_acceptance: boolean;
  adulthood_declaration: boolean;
};

/**
 * Whether any consent UI is drawn at all. A Customer who owes nothing sees no
 * Short Notice, no boxes and no link: the checkout is exactly what it was before
 * this feature existed (parent #249, user story 10), which since #385 is the
 * ordinary case rather than the reward for a returning buyer.
 *
 * THE DECLARATION IS NAMED HERE EVEN THOUGH IT CANNOT DECIDE THE ANSWER, and
 * that is a deliberate difference from the API's own reading of the same field
 * (consent.Outstanding.Any, which leaves it out to keep the tracking rule
 * legible). What differs is the consequence of being wrong. There, an omitted
 * term changes nothing about who is stopped; here, a box the API owes and this
 * section does not draw is a submit button disabled forever beside a checkbox
 * that is not on the screen — the dead end this whole capture point exists to
 * prevent. Naming it costs one clause and makes the section's presence follow
 * from the boxes rather than from an invariant held somewhere else.
 */
export function anyConsentBox(boxes: ConsentBoxes): boolean {
  return (
    boxes.policy_acceptance ||
    boxes.marketing_consent ||
    boxes.networking_consent ||
    boxes.terms_acceptance ||
    boxes.adulthood_declaration
  );
}
