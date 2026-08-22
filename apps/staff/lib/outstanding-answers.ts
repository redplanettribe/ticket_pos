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
  /*
    THE GUEST LIST (#329, ADR 0047). Who is coming on this Ticket, beside what
    they still owe — the answer this Organization could previously give only as
    the buyer's name repeated once per Ticket.

    EVERY FIELD IS OPTIONAL, AND THAT IS THE FLAG. With
    `TICKET_ASSIGNMENT_ENABLED` closed the API omits all four, so their absence
    IS the closed flag and this app holds no second copy of it (ADR 0045).
  */
  assignment_state?: TicketAssignmentState;
  /*
    The Holder, filled by the API only once that person has ACCEPTED.

    An address a buyer typed and its owner never clicked is reported as
    `assigned` and never named: it has no consent moment behind it, and the
    person may not know a ticket was bought for them (ADR 0047). So a row can
    carry a state and no Holder, and drawing the state is the only way to tell
    that row from one nobody was ever named for.
  */
  holder_first_name?: string;
  holder_last_name?: string;
  holder_email?: string;
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

/**
 * Which of the three states one Ticket's assignment is in (#329, ADR 0046).
 *
 * DERIVED BY THE API AND NEVER HERE. It is read off three columns by
 * `catalog.AssignmentState`, so the buyer's page, this list and the Sales Export
 * cannot mean different things by the word `assigned`. A second derivation on
 * this side would be a fourth opinion and the first one to drift.
 *
 * THREE VALUES AND NEVER FOUR. A Ticket whose unaccepted address the retention
 * purge has taken arrives here as `unassigned` — nobody holds it, which is the
 * truth; what happened to it is the platform's own record and not a state.
 */
export type TicketAssignmentState = "unassigned" | "assigned" | "accepted";

/**
 * The `outstandingAnswers` catalog key each assignment state is named with.
 *
 * Total over the three states, so a state added to the API could not reach this
 * screen as a blank cell.
 */
export const ASSIGNMENT_STATE_KEYS = {
  unassigned: "guestUnassigned",
  assigned: "guestAssigned",
  accepted: "guestAccepted",
} as const satisfies Record<TicketAssignmentState, string>;

/**
 * The Badge variant a state is drawn in, following `promotionStateBadgeVariant`
 * beside it: the state decides the colour in one place, and the table only draws.
 *
 * `accepted` is the finished state and reads as success. `assigned` is waiting on
 * somebody and reads as warning — it is the row an Organizer can still do
 * something about. `unassigned` is the ordinary case and is drawn quietly,
 * because most Tickets are unassigned and a list shouting at every one of them
 * says nothing.
 */
export function assignmentStateBadgeVariant(
  state: TicketAssignmentState,
): "success" | "warning" | "outline" {
  switch (state) {
    case "accepted":
      return "success";
    case "assigned":
      return "warning";
    default:
      return "outline";
  }
}

/**
 * The Holder's name for display, from the two halves the API keeps apart.
 *
 * Joined here for `buyerName`'s reason: the surface that shows a name is where
 * the choice of which part leads belongs, not the payload.
 *
 * EMPTY UNTIL SOMEBODY HAS ACCEPTED, because a name arrives only with acceptance
 * — which is exactly why the state travels beside it.
 */
export function holderName(ticket: TicketOwingAnswers): string {
  return [ticket.holder_first_name ?? "", ticket.holder_last_name ?? ""]
    .map((part) => part.trim())
    .filter(Boolean)
    .join(" ");
}

/**
 * Whether the guest list has anything to draw at all.
 *
 * THE FLAG IS READ OFF THE PAYLOAD'S ABSENCE AND NOWHERE ELSE, exactly as the
 * Storefront reads it (ADR 0045). With `TICKET_ASSIGNMENT_ENABLED` closed the API
 * omits `assignment_state` from every row, so this app needs no second copy of a
 * deployment flag it cannot see — and the column disappears rather than filling a
 * screen with a word nobody can act on.
 */
export function guestListVisible(rows: readonly TicketOwingAnswers[]): boolean {
  return rows.some((ticket) => Boolean(ticket.assignment_state));
}
