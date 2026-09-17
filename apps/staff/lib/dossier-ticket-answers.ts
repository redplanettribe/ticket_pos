/**
 * One Ticket's Answers on the Customer Dossier (#641): what it answered, the
 * Outstanding Answers it still owes, and when an Answer Reminder was last sent
 * about it.
 *
 * THE API DECIDES EVERY FACT. Which questions are owed is the Holder List's own
 * computation, run server-side, so the two screens cannot disagree about one
 * Ticket; this module only reads the payload's shape. ABSENCE IS THE FLAG: with
 * TICKET_QUESTIONS_ENABLED closed the API omits all three keys, and on a Ticket
 * of a Sale that no longer stands it omits the debt and the reminder while
 * keeping the Answers readable (ADR 0045).
 *
 * Tokens and shapes, never words: the component draws them from the
 * `customerDossier` catalog namespace (ADR 0041). Question and Option labels
 * are the Organization's own words and are drawn as coined.
 */

import type { OutstandingQuestion } from "./holder-list.ts";
import { hasAnswer, type TicketQuestionAnswer } from "./ticket-answers.ts";

/** The three optional keys #641 adds to a Dossier Ticket and a held Ticket. */
export type DossierTicketAnswerFields = {
  answers?: TicketQuestionAnswer[];
  outstanding_answers?: OutstandingQuestion[];
  last_answer_reminder_sent_at?: string | null;
};

/** Whether the Ticket's Answers are drawn at all: the questions flag, off the payload. */
export function ticketAnswersVisible(ticket: Pick<DossierTicketAnswerFields, "answers">): boolean {
  return ticket.answers !== undefined;
}

/**
 * The Ticket's answered pairs, in the order the API gave them. The API sends
 * only answered ones; an unanswered pair is dropped here too, so a change there
 * never draws an empty value as if it were an Answer.
 */
export function answeredPairs(ticket: Pick<DossierTicketAnswerFields, "answers">): TicketQuestionAnswer[] {
  return (ticket.answers ?? []).filter(hasAnswer);
}

/**
 * What the Ticket owes. `hidden` when the key is absent — the flag closed, or a
 * Sale that no longer stands, whose Tickets owe nothing and are not chased.
 */
export type TicketOwes =
  | { state: "hidden" }
  | { state: "nothing" }
  | { state: "owes"; questions: OutstandingQuestion[] };

export function ticketOwes(ticket: Pick<DossierTicketAnswerFields, "outstanding_answers">): TicketOwes {
  const questions = ticket.outstanding_answers;
  if (questions === undefined) {
    return { state: "hidden" };
  }
  return questions.length === 0 ? { state: "nothing" } : { state: "owes", questions };
}

/** When an Answer Reminder was last sent about the Ticket; `hidden` under the same rules as the debt. */
export type LastAnswerReminder = { state: "hidden" } | { state: "never" } | { state: "sent"; at: string };

export function lastAnswerReminder(
  ticket: Pick<DossierTicketAnswerFields, "last_answer_reminder_sent_at">,
): LastAnswerReminder {
  const at = ticket.last_answer_reminder_sent_at;
  if (at === undefined) {
    return { state: "hidden" };
  }
  return at === null ? { state: "never" } : { state: "sent", at };
}

/**
 * An Answer's value, read by its question's kind and never guessed from the
 * value. `unreadable` when the shape does not fit the kind, so the component
 * draws a placeholder rather than a wrong reading.
 *
 * A number stays the API's decimal string; a date stays its YYYY-MM-DD day, for
 * `formatCalendarDay`; a choice reads each Option's SNAPSHOT label, the words the
 * person actually chose from.
 */
export type AnsweredValue =
  | { kind: "text"; text: string }
  | { kind: "number"; digits: string }
  | { kind: "date"; day: string }
  | { kind: "checkbox"; checked: boolean }
  | { kind: "choice"; labels: string[] }
  | { kind: "unreadable" };

export function answeredValue(pair: TicketQuestionAnswer): AnsweredValue {
  const answer = pair.answer;
  if (!answer) {
    return { kind: "unreadable" };
  }
  switch (pair.question.kind) {
    case "short_text":
    case "long_text":
      return answer.text !== null ? { kind: "text", text: answer.text } : { kind: "unreadable" };
    case "number":
      return answer.number !== null ? { kind: "number", digits: answer.number } : { kind: "unreadable" };
    case "date":
      return answer.date !== null ? { kind: "date", day: answer.date } : { kind: "unreadable" };
    case "checkbox":
      return answer.checked !== null ? { kind: "checkbox", checked: answer.checked } : { kind: "unreadable" };
    case "single_choice":
    case "multi_choice":
      return answer.options.length > 0
        ? { kind: "choice", labels: answer.options.map((option) => option.label) }
        : { kind: "unreadable" };
    default:
      return { kind: "unreadable" };
  }
}
