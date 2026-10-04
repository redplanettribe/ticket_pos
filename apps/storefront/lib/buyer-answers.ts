/**
 * The buyer's own view of the Tickets of their Sale (#315, ADR 0044; narrowed
 * by #344, ADR 0049), as rules rather than as markup.
 *
 * THE SURFACE THIS SPEAKS FOR IS ONE PAGE AND NOT TWO. The Confirmation Link
 * does not open a page of its own — it redeems into a Customer Session narrowed
 * to one Ticket Sale and lands on the Customer Area, which lists every purchase
 * the session can reach. So "the page behind the Confirmation Link" and "the
 * Customer Area" differ only in how many sales are on them, and both render
 * from this.
 *
 * WHAT THE BUYER DOES HERE, since ADR 0049: says whose each Ticket is, and
 * answers the ONE Ticket they hold. An Answer is given only by a Ticket's
 * Holder — the buyer for their Self-held Ticket (ADR 0048) — or by Event
 * Staff. Of a Ticket they do not hold the buyer sees its position, its Ticket
 * Type and its assignment state, and nothing else: no question rows, no
 * Answered/Outstanding badge, no link. The Answer Link is retired.
 *
 * ONE EXCEPTION, SINCE ADR 0076 (#672): on an Event that requires Named
 * Tickets, a Ticket assigned to somebody who has not yet accepted carries the
 * Answers the buyer gave at checkout on its sale-scoped row, and the buyer may
 * correct them until that person accepts. See `provisionalRowFor`.
 *
 * TWO PAYLOADS, ONE PANEL. The sale-scoped list (`BuyerTicket`) carries every
 * Ticket of the Sale with its assignment fields and NO answer fields; the
 * held-ticket list (`HeldTicket`) carries the Tickets this Customer holds with
 * their questions and Answers and NOTHING about the Sale. The buyer's page
 * draws one row per sale-scoped Ticket and, for the row that is self-held,
 * finds its questions in the held list by `ticket_id`. The rules below are the
 * joins between the two and the two "draw nothing at all" decisions.
 *
 * IT REUSES lib/ticket-questions.ts RATHER THAN RESTATING IT. A Ticket Question, an
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

// Relative and with its extension so the test runner (node --test, no path
// aliases) can resolve the one runtime import, as lib/held-ticket-panel.ts does.
import { visibleQuestionsOf, type QuestionAnswer } from "./ticket-questions.ts";

export type { Answer, AnswerBody, Question, QuestionAnswer, QuestionKind } from "@/lib/ticket-questions";

/**
 * One Ticket of the buyer's own Ticket Sale, as the sale-scoped list sends it:
 * which one it is, and whose it is.
 *
 * IT CARRIES NO QUESTION, NO ANSWER, NO OUTSTANDING COUNT AND NO LINK, for any
 * row — the Self-held one included. The API dropped them (ADR 0049), and this
 * type dropping them too is what keeps a row the buyer does not hold from ever
 * being asked to render a question it was never sent.
 */
export type BuyerTicket = {
  ticket_id: string;
  /**
   * Which of its line's units this is, 1..quantity. The buyer's handle on
   * which of four identical tickets is which until an address is given —
   * there are no seat numbers and no holder names, so "ticket 2 of 4" is the
   * whole of what can be said to tell them apart.
   */
  ordinal: number;
  ticket_type_name: string;
  /**
   * THE TICKET ASSIGNMENT (#324), and every one of these is OPTIONAL for one
   * reason: the API omits the lot while TICKET_ASSIGNMENT_ENABLED is closed, so
   * the payload a dark deployment sends is byte-identical to the one a build
   * without the feature sends. `assignment_state === undefined` is therefore how
   * this app reads the flag, and it is the only place it reads it — see
   * lib/ticket-assignment.ts, which owns every rule about these fields.
   *
   * They are declared HERE, on the row they arrive on, and interpreted THERE.
   */
  assignment_state?: string;
  /** The address this Ticket was assigned to, shown back to the buyer who typed
   * it and to nobody else on any surface. Absent while unassigned. */
  holder_email?: string;
  assigned_at?: string;
  accepted_at?: string;
  /** Whether this Ticket's Holder is the buyer themself — the one the sale
   * handed them at purchase (ADR 0048). The page says "your ticket" on it and
   * draws its questions from the held list. */
  self_held?: boolean;
  /** Whether an address may be given or changed right now, and the token saying
   * why not. */
  assignable?: boolean;
  assignable_refusal?: string;
  /**
   * THE ONE EXCEPTION TO "NO QUESTION ON THIS ROW" (#672, ADR 0076). On an
   * Event that requires Named Tickets, a Ticket `assigned` to somebody who has
   * not yet accepted carries the questions and the Answers the buyer gave at
   * checkout, which stay the buyer's to correct until that person accepts.
   * Absent on every other row, and gone from this one once they accept: from
   * then the Answers are theirs. Read through `provisionalRowFor`.
   */
  provisional_answers?: ProvisionalAnswers;
};

/**
 * An assigned Ticket's questions and Answers as its buyer reads them on a
 * Named Tickets Event: the answering half of a held row, under the same names,
 * so one panel draws either.
 */
export type ProvisionalAnswers = {
  answerable: boolean;
  answerable_refusal: string;
  outstanding_count: number;
  questions: QuestionAnswer[];
};

/**
 * What the answering panel needs of a Ticket, whichever route it is answered
 * through: a held row, or a buyer's row carrying provisional Answers.
 */
export type AnswerPanelTicket = ProvisionalAnswers & {
  ticket_id: string;
  ticket_type_name: string;
};

/**
 * One Ticket the Customer HOLDS, as `/api/customer/held-tickets` sends it
 * (#343, ADR 0049): its questions, its Answers and what it still owes — and
 * nothing about the Sale, because the same payload serves a Holder who was
 * given the Ticket and is entitled to nothing about the purchase.
 */
export type HeldTicket = {
  ticket_id: string;
  event_name: string;
  event_slug: string;
  ticket_type_name: string;
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
 * The held-ticket row behind one of the buyer's sale-scoped rows, or null.
 *
 * NULL FOR EVERY TICKET THE BUYER DOES NOT HOLD, which is most of them, and
 * null for a Self-held Ticket whose held row has not arrived or was refused.
 * Either way the row draws as an assignment row and nothing more: a question
 * this page has not been sent is a question it has no business drawing.
 *
 * Joined on `ticket_id` and NOT on `self_held`: the flag says the buyer holds
 * it, the held list says what it asks, and a Ticket on one side and not the
 * other — the buyer gave it away between the two fetches — is drawn as an
 * ordinary assignable row rather than as a panel with no questions.
 */
export function heldRowFor(ticket: BuyerTicket, held: HeldTicket[]): HeldTicket | null {
  if (ticket.self_held !== true) return null;
  return held.find((row) => row.ticket_id === ticket.ticket_id) ?? null;
}

/**
 * Whether a Ticket the buyer holds has no row on the held list yet — the
 * signal to read the held list again.
 *
 * It happens when the buyer assigns a Ticket to their own address: the Ticket
 * is held the moment the write returns (ADR 0076), but its questions live on
 * the held list, which this page read before. Reading it again turns the row
 * into the buyer's own panel at once rather than on the next page load.
 */
export function heldListMissesAHeldTicket(tickets: BuyerTicket[], held: HeldTicket[]): boolean {
  return tickets.some(
    (ticket) => ticket.self_held === true && !held.some((row) => row.ticket_id === ticket.ticket_id),
  );
}

/**
 * Whether the buyer's one answerable row has anything to answer — i.e. whether
 * this sale has a question half at all.
 *
 * A SALE WHOSE TICKET TYPES ASK NOTHING GETS NO QUESTION HALF, not an empty
 * one. Most Organizations have never written a Ticket Question, so most
 * Customer Areas must look exactly as they did before this feature — and a
 * buyer who holds no Ticket of the Sale (gave theirs away, or bought at the
 * door) has no questions here however many the Ticket Type asks: those are
 * their Holders' to answer.
 *
 * IT SPEAKS FOR TICKET QUESTIONS ONLY, and deliberately says nothing about
 * Ticket Assignment (#324), which is behind its own flag and can be the sole
 * reason to draw the section. The caller asks both — see `saleOffersAssignment`
 * in lib/ticket-assignment.ts.
 */
export function hasAnythingToShow(tickets: BuyerTicket[], held: HeldTicket[]): boolean {
  return tickets.some((ticket) => {
    const row = heldRowFor(ticket, held);
    return (row !== null && row.questions.length > 0) || provisionalRowFor(ticket) !== null;
  });
}

/**
 * The answering panel behind a row whose Answers are provisionally the
 * buyer's (#672, ADR 0076), or null.
 *
 * NULL UNLESS THE API SENT THE BLOCK. Whether the buyer may read and correct
 * a Ticket's Answers is the API's decision - the Event requires Named Tickets
 * and the Ticket is assigned and unaccepted - and never this page's guess
 * from the assignment state: a page that decided for itself could draw a
 * Holder's Answers to somebody who no longer owns them.
 *
 * NULL TOO WHEN NOTHING WOULD BE DRAWN - no question, or only retired ones
 * this Ticket never answered - so the row is an ordinary assignment row
 * rather than a panel promising something empty, the rule the held panel
 * follows.
 */
export function provisionalRowFor(ticket: BuyerTicket): AnswerPanelTicket | null {
  const answers = ticket.provisional_answers;
  if (answers === undefined || visibleQuestionsOf(answers.questions).length === 0) return null;
  return { ticket_id: ticket.ticket_id, ticket_type_name: ticket.ticket_type_name, ...answers };
}

/**
 * The Outstanding count one Ticket's provisional Answers carry on a Sale the
 * provisional write just handed back - what decides whether the panel that
 * saved folds. Zero when the row came back without the block (its Holder
 * accepted meanwhile): it owes the buyer nothing any more.
 */
export function provisionalOutstandingIn(tickets: BuyerTicket[], ticketId: string): number {
  return tickets.find((ticket) => ticket.ticket_id === ticketId)?.provisional_answers?.outstanding_count ?? 0;
}

/**
 * How many Outstanding Answers the buyer owes on this sale — DEBTS AND NOT
 * TICKETS, on the Tickets whose Answers are the buyer's to give: the one they
 * hold, and on a Named Tickets Event the ones still waiting on their Holder
 * (ADR 0076). The same unit the Organization's own headline figure uses, so a
 * buyer on the phone and the member of staff they are talking to are
 * counting the same things. Zero when none are theirs.
 */
export function saleOutstandingCount(tickets: BuyerTicket[], held: HeldTicket[]): number {
  return tickets.reduce(
    (total, ticket) =>
      total +
      (heldRowFor(ticket, held)?.outstanding_count ?? 0) +
      (provisionalRowFor(ticket)?.outstanding_count ?? 0),
    0,
  );
}

/**
 * The held list with one row replaced by what the API just sent back from a
 * write. The held write returns ONE Ticket, not the list — each held Ticket's
 * panel stands alone — so the page patches it in by id rather than refetching.
 * A Ticket not already in the list is appended, so a write never loses a row.
 */
export function withHeldRow(held: HeldTicket[], updated: HeldTicket): HeldTicket[] {
  const index = held.findIndex((row) => row.ticket_id === updated.ticket_id);
  if (index === -1) return [...held, updated];
  return held.map((row, i) => (i === index ? updated : row));
}

/**
 * How many placeholder rows to reserve while the two fetches are in flight
 * (#357). One per Ticket on the Sale — the count the Sale's own lines already
 * know, so the space is held before either list has arrived — and never fewer
 * than one: a Sale with no countable lines (a malformed or zero-quantity
 * payload) still deserves a row's worth of room rather than a heading over
 * nothing. Never more than a dozen: past that the skeleton is a wall of grey
 * and the reader gains nothing from its exactness.
 */
export function placeholderRowCount(ticketCount: number): number {
  if (!Number.isFinite(ticketCount)) return 1;
  return Math.min(12, Math.max(1, Math.floor(ticketCount)));
}
