import type { AppLocale } from "@ticket-pos/locale";

/**
 * The publish step's rules, as plain functions over plain data (#563, spec
 * #556).
 *
 * A SIBLING OF legal-drafts.ts AND NOT A PART OF IT. That module is about a
 * DRAFT — what is written, what is missing, what changed. This one is about the
 * ACT: which of the two publications is offered, and why the other one is not.
 * They are different questions with different readers, and the split keeps
 * "is this a structural change" (there) from acquiring a second answer here.
 *
 * NOTHING HERE REFUSES ANYTHING. Every rule below is also enforced at the HTTP
 * seam, and the seam is the guarantee: a precondition a browser could decline to
 * check is not a precondition. What these functions decide is what the SCREEN
 * says — which button is live, and what the disabled one's title reads — so that
 * an operator meets a refusal while they can still act on it rather than after
 * pressing a button.
 *
 * NO REACT AND NO NETWORK, for legal-drafts.ts's reason: a rule that decides
 * whether a legal document may be published is not something to verify by
 * rendering a component and reading a coloured dot back out of the DOM.
 */

/** The two acts. There is no third, and no default (#553 removed promotion). */
export type PublishKind = "edition" | "correction";

/**
 * The three gates, and there are exactly three:
 *
 *     canPublish = gaps.complete && previewedAll && seenDiff
 *
 * Complete, because no reader may meet a document with a hole in it. Previewed,
 * because "I only ever saw it in a textarea" is one of the two ways a
 * publication goes wrong quietly. Diff seen, because "I did not know what it
 * changed" is the other. Together they are THE SUBSTITUTE FOR A SECOND PAIR OF
 * EYES — production holds one Platform Operator, so there is nobody to approve
 * anything, and what the platform can prove instead is that the operator was
 * SHOWN the consequence.
 */
export function canPublishDraft(gates: {
  complete: boolean;
  previewedAll: boolean;
  seenDiff: boolean;
}): boolean {
  return gates.complete && gates.previewedAll && gates.seenDiff;
}

/**
 * Why the CORRECTION is not on offer, or null when it is.
 *
 * ORDERED MOST SPECIFIC FIRST, because the disabled link has room for one
 * sentence and the useful one is the sentence about this draft rather than the
 * generic one about the gates. A draft that both adds an artifact and has not
 * been previewed is told about the artifact: that is the fact the operator has
 * to decide something about, and the previewing is work they were going to do
 * anyway.
 */
export type CorrectionBlocker = "structural" | "locales" | "empty" | "gates";

export function correctionBlocker(state: {
  canPublish: boolean;
  structural: boolean;
  localeSetChanged: boolean;
  emptyDiff: boolean;
}): CorrectionBlocker | null {
  if (state.structural) return "structural";
  if (state.localeSetChanged) return "locales";
  if (state.emptyDiff) return "empty";
  if (!state.canPublish) return "gates";
  return null;
}

/**
 * Whether the draft would publish a different SET of languages from the edition
 * it replaces.
 *
 * A set comparison and not a list one: order is not a fact about a published
 * language set, and a draft that names the same two languages in the other order
 * has changed nothing. Refuses a correction, because the locale set is part of
 * the hash preimage — adding or dropping a language RESHAPES what is hashed
 * rather than merely changing what is hashed.
 */
export function localeSetChanged(
  published: readonly AppLocale[],
  draft: readonly AppLocale[],
): boolean {
  const before = new Set(published);
  const after = new Set(draft);
  if (before.size !== after.size) return true;
  for (const locale of before) {
    if (!after.has(locale)) return true;
  }
  return false;
}

/**
 * Whether the draft would stop publishing the language its document may not be
 * published without.
 *
 * The language itself comes from the API, because it is a CONSTANT OF THE
 * DOCUMENT'S OWN PACKAGE and never a column — a column would be settable, and
 * "unpublish Spanish" would become "set the column, then unpublish Spanish"
 * through this same editor by the same one person. The two REASONS are copy and
 * live in the message catalogs, one per document, because they rest on two
 * different footings: a statute for the notice, a clause of the contract for the
 * agreement.
 */
export function protectedLocaleDropped(
  protectedLocale: AppLocale,
  draft: readonly AppLocale[],
): boolean {
  return !draft.includes(protectedLocale);
}

/**
 * The number as the confirm button says it: grouped, in the reader's own
 * language, so "1,058" is read as a thousand people and not glanced past as
 * "1058".
 *
 * It is the one number on this screen that has to land, because it is the whole
 * of what the operator is being shown before they accept an irreversible act —
 * and the same figure is stored on the version row, so what they saw is what the
 * record says they saw.
 */
export function formatHeadcount(headcount: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(headcount);
}
