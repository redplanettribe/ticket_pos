/**
 * The buyer's own view of their Tickets' Ticket Questions (#315, ADR 0044), as
 * rules rather than as markup.
 *
 * THE SURFACE THIS SPEAKS FOR IS ONE PAGE AND NOT TWO. The Confirmation Link
 * does not open a page of its own — it redeems into a Customer Session narrowed
 * to one Ticket Sale and lands on the Customer Area, which lists every purchase
 * the session can reach. So "the page behind the Confirmation Link" and "the
 * Customer Area" differ only in how many sales are on them, and both render
 * from this.
 *
 * WHAT THE BUYER DOES HERE that nobody else can: distribute. A buyer who bought
 * four tickets knows one t-shirt size and not the other three, and the platform
 * holds no address for the other three people and asks for none (ADR 0044). So
 * they answer what they know and copy the rest of the links into whatever they
 * already use. Every Ticket carries its own link, and the link opens that Ticket
 * and nothing else.
 *
 * IT REUSES lib/answer-link.ts RATHER THAN RESTATING IT. A Ticket Question, an
 * Answer and the body that writes one are the same shapes on every surface that
 * touches them, and the rules for turning a form into a payload are the same
 * rules — a second copy would be a second opinion about what `checked: false`
 * means, or about whether an empty field is an Answer of "".
 *
 * Pure and dependency-free — no React, no i18n runtime — so it is directly
 * unit-testable, exactly as its sibling is. It returns SHAPES and never a
 * sentence.
 *
 * WHAT IS DATA AND WHAT IS COPY. A Ticket Question's label and an Option's
 * label are read AS COINED in every Locale, like a Custom Tag (ADR 0027). Only
 * the page's own chrome follows the reader's Locale, and messages/README.md is
 * explicit that a question's words must never enter the catalog.
 */

import type { QuestionAnswer } from "@/lib/answer-link";

export type { Answer, AnswerBody, Question, QuestionAnswer, QuestionKind } from "@/lib/answer-link";

/**
 * One Ticket of the buyer's own Ticket Sale.
 *
 * IT CARRIES AN ANSWER LINK AND THE ANSWER LINK'S OWN VIEW DOES NOT, which is
 * the whole difference between this type and `AnswerLinkView`. That one is what
 * a forwarded stranger sees and is deliberately three fields wide; this one is
 * what the person who paid sees, and the link is the field they came for.
 */
export type BuyerTicket = {
  ticket_id: string;
  /**
   * Which of its line's units this is, 1..quantity. The buyer's ONLY handle on
   * which of four identical tickets they are copying a link for — there are no
   * seat numbers and no holder names, so "ticket 2 of 4" is the whole of what
   * can be said to tell them apart.
   */
  ordinal: number;
  ticket_type_name: string;
  /**
   * The link to pass to whoever will use this ticket.
   *
   * EMPTY MEANS THERE IS NO LINK TO GIVE, never "not loaded yet". The API
   * withholds it once the Ticket can no longer be answered, because a copy
   * button is a promise: somebody who pastes a dead link into a group chat has
   * finished the task as far as they know and will never find out otherwise.
   */
  answer_link: string;
  /** Whether Answers may still be written. False after the Event starts and on
   * a reversed Ticket Sale — and never a reason to hide what was said. */
  answerable: boolean;
  /** Why not, or "" while it is answerable. */
  answerable_refusal: string;
  /**
   * How many required Ticket Questions this Ticket still owes, from the
   * platform's ONE definition of an Outstanding Answer (#313). Never recomputed
   * on this side: a browser counting for itself would be a third opinion about
   * a debt that already, deliberately, has two.
   */
  outstanding_count: number;
  questions: QuestionAnswer[];
};

/**
 * Whether this Ticket Sale has anything outstanding at all.
 *
 * The page leads with this, and the Sale Confirmation's one conditional sentence
 * is the same fact decided on the server. They are allowed to disagree for as
 * long as it takes somebody to answer a question after opening their email —
 * which is exactly why the mail names no number and this page does.
 */
export function saleHasOutstandingAnswers(tickets: BuyerTicket[]): boolean {
  return tickets.some((ticket) => ticket.outstanding_count > 0);
}

/**
 * How many Outstanding Answers the whole Ticket Sale carries — DEBTS AND NOT
 * TICKETS, so a Ticket owing three counts three. The same unit the
 * Organization's own headline figure uses, so a buyer on the phone and the
 * member of staff they are talking to are counting the same things.
 */
export function saleOutstandingCount(tickets: BuyerTicket[]): number {
  return tickets.reduce((total, ticket) => total + ticket.outstanding_count, 0);
}

/**
 * Whether this Ticket has a link worth offering a copy button for.
 *
 * Two conditions and not one: there must BE a link, and there must be something
 * to use it for. A Ticket whose questions are all answered still has a valid
 * link — the holder may correct what the buyer guessed — so this does not check
 * the outstanding count. What it checks is that the API gave us a link at all.
 */
export function hasAnswerLink(ticket: BuyerTicket): boolean {
  return ticket.answer_link !== "";
}

/**
 * Whether a Ticket Sale is worth drawing this section for at all.
 *
 * A SALE WHOSE TICKET TYPES ASK NOTHING GETS NO SECTION, not an empty one. Most
 * Organizations have never written a Ticket Question, so most Customer Areas
 * must look exactly as they did before this feature — an empty "Ticket
 * questions" heading on every purchase anybody ever made would be the feature
 * announcing itself to the people it has nothing to say to.
 */
export function hasAnythingToShow(tickets: BuyerTicket[]): boolean {
  return tickets.some((ticket) => ticket.questions.length > 0);
}
