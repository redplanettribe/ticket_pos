/**
 * The Event's Outstanding Answers as the staff surface reads them (#313).
 *
 * An OUTSTANDING ANSWER is a required Ticket Question that one Ticket has not
 * answered yet — a debt, not a defect, and the whole meaning of "required" on
 * this platform. Nothing anywhere was refused for want of one, on any channel,
 * so the only thing an Organization can do about a missing size is see that it
 * is missing and chase it before it orders the shirts.
 *
 * NOTHING HERE DECIDES WHAT IS OUTSTANDING. The definition lives once, in the
 * API — `catalog.IsOutstandingAnswer` and the SQL beside it — and this module
 * only reads what came back. A copy of the rule on this side would be a third
 * statement of it, and the one most likely to drift: it would have to guess at
 * `required`, at retirement and at the Ticket Sale's status from a payload that
 * has already applied all three.
 *
 * Pure and dependency-free apart from the shared fetch helper, so the shaping is
 * unit-testable under the fast runner.
 */

import { fetchEventsJSON } from "./events-api.ts";
import type { TicketQuestionKind } from "./ticket-questions.ts";

/**
 * The Sales Channel a Ticket's sale came through.
 *
 * It EXPLAINS a row rather than filtering it. A door sale and a Sale Import
 * start out owing every question, because nobody ever put the questions to those
 * buyers — there is no checkout form on either — and a surface that could not say
 * so would read as lost data rather than as history.
 */
export type SalesChannel = "online" | "in_person" | "import";

/** One Outstanding Answer: a required question this Ticket has not answered. */
export type OutstandingQuestion = {
  question_id: string;
  /**
   * The Organization's own words. Data, never copy: rendered as coined in both
   * languages, exactly as a Ticket Type name is.
   */
  label: string;
  kind: TicketQuestionKind;
  sort_order: number;
};

/** One Ticket that still owes, and everything a chase or a jump needs. */
export type TicketOwingAnswers = {
  ticket_id: string;
  /**
   * Which of its Ticket Sale Line's units this Ticket is, 1..quantity. Internal
   * and not a seat number, but the only thing telling two Tickets of one line
   * apart — which is what lets staff say "the second of Ana's four".
   */
  ordinal: number;
  ticket_type_id: string;
  ticket_type_name: string;
  /**
   * The Ticket Sale is how this row is ACTED ON: the Answers dialog is keyed on
   * one and names itself after the buyer's reference.
   */
  ticket_sale_id: string;
  confirmation_ref: string;
  channel: SalesChannel;
  customer_first_name: string;
  customer_last_name: string;
  customer_email: string;
  sold_at: string;
  /** Never empty — a Ticket owing nothing is not on this list at all. */
  outstanding: OutstandingQuestion[];
};

export type OutstandingAnswersPage = {
  data: TicketOwingAnswers[];
  pagination: {
    page: number;
    page_size: number;
    /** How many TICKETS owe something, across the whole Event. */
    total: number;
    total_pages: number;
  };
  /**
   * How many Outstanding Answers the Event carries in ALL — debts, not Tickets,
   * so a Ticket owing three counts three. A second number beside `total` because
   * the two answer different questions: how many people are waiting on the
   * Organization, and how many things it does not yet know.
   */
  outstanding_count: number;
};

/** The default page size, matching the API's own. */
export const OUTSTANDING_PAGE_SIZE = 50;

/**
 * Reads a page of the Event's Outstanding Answers.
 *
 * The page is a parameter and not a filter set: this list deliberately offers
 * nothing to narrow it by. Every row on it is a thing the Organization does not
 * know, and a filter would only ever be a way to look at fewer of them.
 */
export async function fetchOutstandingAnswers(
  eventId: string,
  page: number,
): Promise<OutstandingAnswersPage> {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(OUTSTANDING_PAGE_SIZE),
  });
  return fetchEventsJSON<OutstandingAnswersPage>(
    `/api/events/${eventId}/outstanding-answers?${query.toString()}`,
  );
}

/**
 * The `sales` catalog key a Sales Channel is named with.
 *
 * Reused from the Sales list rather than re-coined, for the reason
 * `TICKET_QUESTION_KIND_KEYS` exists: a channel translated per screen is how
 * there come to be two Spanish words for "door sale".
 */
export const SALES_CHANNEL_KEYS = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
} as const satisfies Record<SalesChannel, string>;

/**
 * A buyer's name for display, from the two halves the API keeps apart.
 *
 * Joined here and not in the payload because this is the only place that has to
 * choose an order for them, and a surface that shows a name is exactly where
 * that choice belongs. Falls back to whichever half is present rather than
 * rendering a stray space.
 */
export function buyerName(ticket: TicketOwingAnswers): string {
  return [ticket.customer_first_name, ticket.customer_last_name]
    .map((part) => part.trim())
    .filter(Boolean)
    .join(" ");
}
