/**
 * The held-Ticket panel's rules (#345, ADR 0049), as decisions rather than as
 * markup: when it is open, when it folds, and what press saves an Answer.
 *
 * ONE PANEL FOR THE BUYER AND THE HOLDER. The buyer's "Your ticket" on their
 * sale page and a Holder's Ticket in their own Customer Area are the same
 * panel over the same held-ticket payload, so these rules are written once,
 * here, and both surfaces read them. A second opinion about whether an
 * unrequired question holds the panel open would be a second platform.
 *
 * Pure and dependency-free — no React, no i18n — so it is unit-testable as its
 * siblings lib/buyer-answers.ts and lib/ticket-assignment.ts are.
 */

// Relative and with its extension so the test runner (node --test, no path
// aliases) can resolve it, as lib/undo-window.ts does.
import { visibleQuestionsOf, type QuestionAnswer, type QuestionKind } from "./answer-link.ts";

/**
 * What the panel's header offers.
 *
 * - `none`: the Ticket asks nothing, so there is nothing to expand and no
 *   control promising it (story 13).
 * - `open`: an Answer is owed — the panel starts expanded and the organizer's
 *   wait is in the reader's face (story 7).
 * - `collapsed`: nothing is owed; the panel sits behind "Answered · Review or
 *   edit" and expands to the same editable form (story 11).
 */
export type PanelDisclosure = "none" | "open" | "collapsed";

/**
 * The panel's disclosure from the Ticket's own facts.
 *
 * THE OUTSTANDING COUNT IS THE API'S AND IS NOT RECOMPUTED HERE. It counts
 * REQUIRED questions without an Answer — the platform's one definition of an
 * Outstanding Answer (#313) — which is exactly why an unrequired, unanswered
 * question never holds the panel open (story 12): it is not in the count.
 *
 * "Asks nothing" is judged on the questions the panel would DRAW: a retired
 * question this Ticket never answered is not drawn, so a Ticket whose every
 * question has been retired unanswered has no expand control either.
 */
export function panelDisclosure(ticket: {
  outstanding_count: number;
  questions: QuestionAnswer[];
}): PanelDisclosure {
  if (visibleQuestionsOf(ticket.questions).length === 0) return "none";
  return ticket.outstanding_count > 0 ? "open" : "collapsed";
}

/**
 * Whether a save that took the Outstanding count from `before` to `after`
 * folds the panel (story 10).
 *
 * ONLY THE LAST OWED ANSWER FOLDS IT. A panel the reader opened themself to
 * review owes nothing before the save and nothing after, and a save from
 * there must not snap it shut under their hands; and a save that still leaves
 * something owed keeps the panel open on what remains.
 */
export function collapsesAfterSave(before: number, after: number): boolean {
  return before > 0 && after === 0;
}

/**
 * What press persists an Answer of this kind.
 *
 * A choice, a tick box and a date are each complete the moment they are
 * given, so they save on CHANGE. Text and a number are typed, so they save on
 * BLUR — the moment the reader leaves the field — rather than on every
 * keystroke, which would save "Veg", "Vege", "Veget"... against a question
 * whose answer is a fact about a person.
 */
export type SaveTrigger = "change" | "blur";

export function saveTrigger(kind: QuestionKind): SaveTrigger {
  switch (kind) {
    case "short_text":
    case "long_text":
    case "number":
      return "blur";
    case "date":
    case "checkbox":
    case "single_choice":
    case "multi_choice":
      return "change";
  }
}

/**
 * The closed-window token from the held-ticket payload as a message key under
 * `customerArea.answers`, or null while the window is open.
 *
 * The API names WHY with a token (`event_started`, `sale_reversed`), and the
 * panel says it in the reader's language. An unknown token gets the generic
 * line rather than a blank: the window is shut either way.
 */
export function closedWindowKey(ticket: {
  answerable: boolean;
  answerable_refusal: string;
}): "closedStarted" | "closedReversed" | "closedUnknown" | null {
  if (ticket.answerable) return null;
  switch (ticket.answerable_refusal) {
    case "event_started":
      return "closedStarted";
    case "sale_reversed":
      return "closedReversed";
    default:
      return "closedUnknown";
  }
}
