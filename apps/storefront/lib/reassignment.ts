/**
 * Reassigning a Ticket on an Event that requires Named Tickets, as pure data
 * (ADR 0076, #673).
 *
 * A reassignment clears the old Holder's Answers (ADR 0046), so on such an
 * Event the buyer gives the new Holder's required Answers with the address, or
 * a change of plans would undo the roster checkout was refused until it had.
 * The buyer's own address is no exception: their own Ticket owes its Answers
 * at checkout too. The API judges it and refuses with NAMED_TICKETS_INCOMPLETE,
 * begin-checkout's refusal, naming the questions still owed.
 *
 * THE API SAYS WHEN IT APPLIES. A row that may be reassigned under the rule
 * carries `reassignment_questions`; every other row carries none, and its form
 * is the address alone. So this page never decides for itself whether an Event
 * requires Named Tickets or has started.
 *
 * THE CHECKOUT FORM'S RULES, reused rather than restated: an Answer is usable
 * when `answerIsUsable` says the server would keep it, and a required checkbox
 * reads as the state it is drawn in (`withDrawnCheckboxes`). Like the checkout
 * mirror, this may never ask for more than the server does.
 *
 * Framework-free, so it is unit tested and the row only draws.
 */

import type { BuyerTicket } from "./buyer-answers.ts";
import {
  slotAnswerKey,
  statedReply,
  type AnswerValue,
  type AnswerValues,
  type ExistingTicketSlot,
} from "./checkout-answers.ts";
import { owedQuestionIds, refusedTickets, withDrawnCheckboxes } from "./named-tickets.ts";

/** One entry of the assign body's `answers`: the question, and the reply. */
export type ReassignmentAnswerBody = { ticket_question_id: string } & AnswerValue;

/**
 * The questions a reassignment of this Ticket must answer, as a slot the
 * checkout's answer fields draw, or null when it asks none.
 *
 * KEYED BY THE TICKET'S ID where the checkout keys by Ticket Type: this Ticket
 * already exists, and its id is what tells it apart from the Sale's others on
 * one page.
 */
export function reassignmentSlot(ticket: BuyerTicket): ExistingTicketSlot | null {
  const questions = ticket.reassignment_questions ?? [];
  if (questions.length === 0) return null;
  return {
    ticketId: ticket.ticket_id,
    ticketTypeName: ticket.ticket_type_name,
    ordinal: ticket.ordinal,
    questions,
  };
}

/**
 * The required questions this form has no usable Answer to, in the order they
 * are asked. Empty means the address may be saved. An optional question is
 * never owed.
 */
export function reassignmentOwed(slot: ExistingTicketSlot, values: AnswerValues): string[] {
  return owedQuestionIds(slot, withDrawnCheckboxes([slot], values));
}

/**
 * The body's `answers`: every question something was said to, a required
 * checkbox as it is drawn, and nothing for a blank. Whether a reply fits is the
 * API's finding; it keeps what does, as at checkout.
 */
export function reassignmentAnswerBodies(
  slot: ExistingTicketSlot,
  values: AnswerValues,
): ReassignmentAnswerBody[] {
  const drawn = withDrawnCheckboxes([slot], values);
  const bodies: ReassignmentAnswerBody[] = [];
  for (const question of slot.questions) {
    const stated = statedReply(drawn[slotAnswerKey(slot, question.id)]);
    if (stated === null) continue;
    bodies.push({ ticket_question_id: question.id, ...stated });
  }
  return bodies;
}

/**
 * The questions a NAMED_TICKETS_INCOMPLETE refusal says are still owed. The
 * refusal names one Ticket here, the one in the request; it is read through the
 * checkout's own reader so the two surfaces cannot read the shape differently.
 */
export function refusedQuestionIds(details: unknown): string[] {
  return Object.values(refusedTickets(details)).flatMap((ticket) => ticket.missingQuestionIds);
}
