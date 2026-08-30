/**
 * The Event's Holder List as the staff surface reads it (#333; the Outstanding
 * Answers module of #313, widened to the roster and renamed after it in #519).
 *
 * THE HOLDER LIST IS EVERY TICKET OF THE EVENT: who is coming on each, and —
 * where the Event asks Ticket Questions — which required questions each still
 * owes. A fully answered Ticket stays on it, and an Event that asks nothing
 * still has one, because the roster is the point and the questions are a column
 * on it. OUTSTANDING ANSWERS IS A FILTER of this list, never its definition —
 * the `outstandingOnly` parameter below.
 *
 * An OUTSTANDING ANSWER is a required Ticket Question that one Ticket has not
 * answered yet — a debt, not a defect, and the whole meaning of "required" on
 * this platform. Nothing anywhere was refused for want of one, on any channel.
 *
 * NOTHING HERE DECIDES WHAT IS OUTSTANDING. The definition lives once, in the
 * API — `catalog.IsOutstandingAnswer` and the SQL beside it — and this module
 * only reads what came back. A copy of the rule on this side would be a third
 * statement of it, and the one most likely to drift.
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

/** One Ticket of the Event: its assignment, its buyer, and what it owes. */
export type HolderTicket = {
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
  /**
   * The required questions this Ticket has not answered. An empty array is a
   * Ticket that owes nothing and stays on the roster; the field is ABSENT
   * entirely while `TICKET_QUESTIONS_ENABLED` is closed, so its absence IS that
   * flag (ADR 0045) — the same arrangement the assignment fields have with
   * theirs.
   */
  outstanding?: OutstandingQuestion[];
  /*
    THE HOLDER (#329, ADR 0047). Who is coming on this Ticket — the answer this
    Organization could previously give only as the buyer's name repeated once
    per Ticket.

    EVERY FIELD IS OPTIONAL, AND THAT IS THE FLAG. With
    `TICKET_ASSIGNMENT_ENABLED` closed the API omits all of them, so their
    absence IS the closed flag and this app holds no second copy of it (ADR
    0045).
  */
  assignment_state?: TicketAssignmentState;
  /*
    The purge's marker, presentation-level and derived by the API at read time
    (#334): somebody was named on this Ticket, nobody ever accepted, and the
    retention purge has taken the address. The row arrives as `assigned` with
    this beside it, so the morning-after sheet can tell "nobody was named" from
    "named and never claimed" — and it discloses nothing, because the address
    is gone by definition.
  */
  never_accepted?: boolean;
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

export type HolderListPage = {
  data: HolderTicket[];
  pagination: {
    page: number;
    page_size: number;
    /** How many Tickets the current view holds — the roster, or the owing. */
    total: number;
    total_pages: number;
  };
  /**
   * How many Outstanding Answers the Event carries in ALL — debts, not Tickets,
   * so a Ticket owing three counts three, and unmoved by the filter because it
   * is a fact about the Event. ABSENT while `TICKET_QUESTIONS_ENABLED` is
   * closed, exactly as each row's `outstanding` is.
   */
  outstanding_count?: number;
};

/** The default page size, matching the API's own. */
export const HOLDER_LIST_PAGE_SIZE = 50;

/**
 * Reads a page of the Event's Holder List.
 *
 * `outstandingOnly` is the one filter this list offers, and it is the old list:
 * only the Tickets that still owe a required Answer. The API ignores it while
 * Ticket Questions are dark, so this module does not have to know a flag it
 * cannot see.
 */
export async function fetchHolderList(
  eventId: string,
  page: number,
  outstandingOnly = false,
): Promise<HolderListPage> {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(HOLDER_LIST_PAGE_SIZE),
  });
  if (outstandingOnly) {
    query.set("outstanding", "true");
  }
  return fetchEventsJSON<HolderListPage>(
    `/api/events/${eventId}/holder-list?${query.toString()}`,
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
export function buyerName(ticket: HolderTicket): string {
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
 * purge has taken arrives as `assigned` with `never_accepted` beside it (#334)
 * — a presentation the API derives at read time from the purge marker, not a
 * fourth state.
 */
export type TicketAssignmentState = "unassigned" | "assigned" | "accepted";

/**
 * The `outstandingAnswers` catalog key each assignment state is named with.
 *
 * Total over the three states, so a state added to the API could not reach this
 * screen as a blank cell. A purged row is the one departure, and it is decided
 * in `holderStateKey` below rather than here, because it is a fact beside the
 * state and not a fourth one.
 */
export const ASSIGNMENT_STATE_KEYS = {
  unassigned: "holderUnassigned",
  assigned: "holderAssigned",
  accepted: "holderAccepted",
} as const satisfies Record<TicketAssignmentState, string>;

/**
 * The catalog key one row's assignment is named with: its state's, except that
 * a purged row reads as "assigned, never accepted" (#334) — after the Event
 * every other unaccepted assignment has been overtaken by events, and this one
 * ended without its person. The ONE place the marker changes a word.
 */
export type HolderStateKey =
  | (typeof ASSIGNMENT_STATE_KEYS)[TicketAssignmentState]
  | "holderNeverAccepted";

export function holderStateKey(ticket: HolderTicket): HolderStateKey {
  if (ticket.never_accepted) {
    return "holderNeverAccepted";
  }
  return ASSIGNMENT_STATE_KEYS[ticket.assignment_state ?? "unassigned"];
}

/**
 * The Badge variant a state is drawn in, following `promotionStateBadgeVariant`
 * beside it: the state decides the colour in one place, and the table only draws.
 *
 * `accepted` is the finished state and reads as success. `assigned` is waiting on
 * somebody and reads as warning — it is the row an Organizer can still do
 * something about. `unassigned` is the ordinary case and is drawn quietly,
 * because most Tickets are unassigned and a list shouting at every one of them
 * says nothing. A NEVER-ACCEPTED row is drawn quietly too: it is history, not a
 * chase — nothing anybody does now brings that person.
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

/** The variant for one row, `never_accepted` folded in — see `holderStateKey`. */
export function holderBadgeVariant(ticket: HolderTicket): "success" | "warning" | "outline" {
  if (ticket.never_accepted) {
    return "outline";
  }
  return assignmentStateBadgeVariant(ticket.assignment_state ?? "unassigned");
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
export function holderName(ticket: HolderTicket): string {
  return [ticket.holder_first_name ?? "", ticket.holder_last_name ?? ""]
    .map((part) => part.trim())
    .filter(Boolean)
    .join(" ");
}

/**
 * Whether the Holder column has anything to draw at all.
 *
 * THE FLAG IS READ OFF THE PAYLOAD'S ABSENCE AND NOWHERE ELSE, exactly as the
 * Storefront reads it (ADR 0045). With `TICKET_ASSIGNMENT_ENABLED` closed the API
 * omits `assignment_state` from every row, so this app needs no second copy of a
 * deployment flag it cannot see — and the column disappears rather than filling a
 * screen with a word nobody can act on.
 */
export function holderListVisible(rows: readonly HolderTicket[]): boolean {
  return rows.some((ticket) => Boolean(ticket.assignment_state));
}

/**
 * Whether the questions side of the list exists: the Owes column, the debt
 * summary and the Outstanding Answers filter.
 *
 * The same reading, off the OTHER flag's absence: with
 * `TICKET_QUESTIONS_ENABLED` closed the API omits `outstanding` from every row
 * and `outstanding_count` from the page, and this list is a plain roster.
 */
export function questionsVisible(page: HolderListPage): boolean {
  return page.outstanding_count !== undefined;
}
