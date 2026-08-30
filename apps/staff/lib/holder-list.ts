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

/*
  THE VIEW LIVES IN THE URL (#522, ADR 0065), and everything below exists to
  keep it there.

  `outstanding` was component state for as long as it was the only control: a
  working view of one sitting, no worse for being unaddressable. That reasoning
  does not survive seven filters and a sort — a colleague cannot be sent the
  view, the back button does not undo a narrowing, and, decisively, the HOLDER
  EXPORT CANNOT HONESTLY CLAIM TO MIRROR FILTERS THAT HAVE NO ADDRESS. A file
  read off a hidden view is a file nobody can check.

  This follows the SALES LIST'S SHAPE EXACTLY (`sales-api.ts`: filters type,
  empty value, private append helpers, one query builder, parse-with-fallback).
  A second way to filter a staff table would be two vocabularies for one idea,
  and the pair would drift on the first filter added to either.

  ACCEPTED COST, stated because it is a privacy decision and not an oversight:
  once search arrives (#526) a customer's email address will reach browser
  history and any pasted link. That is accepted ONLY because the Sales list
  already does exactly this; tightening it is one change across both screens,
  not a special case here, and is out of scope of this module.

  ONLY THE FILTERS THAT EXIST TODAY LIVE HERE. #523 added the three structural
  ones — Ticket Type, Sales Channel and the sale's date range — and the
  remaining three are #524–#526, with the five sorts in #527. The types below
  are shaped so those tickets add fields and values without restructuring
  anything, and #523 is the evidence that they do.
*/

/**
 * The Holder List's filters, mirroring the endpoint's query params, one field
 * per dimension.
 *
 * A RECORD AND NOT A BAG OF OPTIONALS, so a filter added by #524–#526 is a
 * compile error everywhere it must be handled rather than a silently-absent
 * key: `EMPTY_HOLDER_LIST_FILTERS`, the append helper and the active check are
 * each total over this type. #523 added three fields under exactly that
 * property and the compiler named every place they had to be handled.
 *
 * `outstanding` is the old Outstanding Answers list reduced to what it always
 * was — one narrowing of the roster. The API ignores it while Ticket Questions
 * are dark, so nothing on this side has to know a flag it cannot see.
 *
 * THE THREE STRUCTURAL FILTERS (#523) narrow the roster by facts about the
 * SALE a Ticket came from, and compose with `outstanding` and with each other:
 * "which VIP door sales are still unclaimed" is one view.
 *
 * WHAT IS DELIBERATELY MISSING IS A STATUS FILTER, and it is missing for a
 * reason a reader will otherwise try to fix: a Sale Reversal means the Tickets
 * CEASE TO EXIST, so reversed Tickets are not rows this list is hiding — they
 * are not rows. There is nothing for such a control to reveal. No payment
 * method and no source either: sale facts with no roster meaning, and the Sales
 * list next door already filters by both (ADR 0065).
 */
export type HolderListFilters = {
  outstanding: boolean;
  /**
   * One Ticket Type's roster — the VIP list apart from general admission. A
   * Ticket belongs to exactly ONE Ticket Type, unlike a Ticket Sale, which is
   * why this narrows differently from the Sales list's control of the same name.
   */
  ticketTypeId: string;
  /**
   * One Sales Channel: `online`, `in_person` or `import`. The channel already
   * explains a row — a door sale owes every question because nobody was ever
   * asked — and this makes it a lever, so "the buyers nobody could ask" is a
   * view rather than a scan.
   */
  channel: string;
  /**
   * A calendar day each, `YYYY-MM-DD`, INCLUSIVE OF BOTH ENDS and read in the
   * EVENT's timezone by the API — so "sold in January" is January where the
   * Event is and not where the reader is standing. Held as the strings a date
   * input produces and never as Dates: a `Date` here would be an instant, and
   * the whole point is that these are days whose length and boundaries the
   * browser is not entitled to decide.
   */
  soldFrom: string;
  soldTo: string;
};

/** The whole roster: every filter at its unfiltered value. */
export const EMPTY_HOLDER_LIST_FILTERS: HolderListFilters = {
  outstanding: false,
  ticketTypeId: "",
  channel: "",
  soldFrom: "",
  soldTo: "",
};

/**
 * The Sales Channels a reader may filter by, in the order the control offers
 * them: the ordinary case first, then the two that were never asked anything.
 *
 * Derived from `SALES_CHANNEL_KEYS` below rather than written twice, so a
 * fourth channel arriving in the type cannot reach the filter without a word to
 * name it — the same totality the key map already has.
 */
export const HOLDER_LIST_CHANNELS: readonly SalesChannel[] = ["online", "in_person", "import"];

/**
 * An allowlisted sort column and direction, carried in the URL and mirroring
 * the backend's own allowlist (ADR 0006) — an unrecognised value must never
 * reach a query.
 *
 * EXACTLY ONE FIELD TODAY. `buyer`, `holder`, `ticket_type` and `owes` are
 * #527's, and land here as a widening of the union and of `HOLDER_SORT_FIELDS`.
 */
export type HolderSortField = "sold_at";
export type HolderSortDir = "asc" | "desc";

export const HOLDER_SORT_FIELDS: readonly HolderSortField[] = ["sold_at"];

export const DEFAULT_HOLDER_SORT: HolderSortField = "sold_at";

/**
 * OLDEST SALE FIRST, and this is not the Sales list's default.
 *
 * The Sales list opens on the newest sale because it is a ledger being watched.
 * The Holder List opens on the oldest because it is a roster being worked
 * through: the earliest buyers have owed their answers longest, and the chase
 * starts at the top. #527 names keeping this order as an explicit criterion, so
 * flipping it here silently re-orders every existing bookmark of this screen.
 */
export const DEFAULT_HOLDER_DIR: HolderSortDir = "asc";

/**
 * Resolves a raw URL value to an allowlisted sort field, falling back to the
 * default. A hand-edited or stale URL stays usable rather than erroring — the
 * backend re-validates regardless, and this is the surface, not the boundary.
 */
export function parseHolderSort(raw: string | undefined): HolderSortField {
  return HOLDER_SORT_FIELDS.includes(raw as HolderSortField)
    ? (raw as HolderSortField)
    : DEFAULT_HOLDER_SORT;
}

/** The same fallback for the direction. */
export function parseHolderDir(raw: string | undefined): HolderSortDir {
  return raw === "asc" || raw === "desc" ? raw : DEFAULT_HOLDER_DIR;
}

/**
 * Writes the non-default filter values onto a params object.
 *
 * PRIVATE ON PURPOSE, and shared by the URL builder and the fetch below: the
 * address bar and the request are built by the same lines, so they cannot
 * disagree about what the reader is looking at. A file that claims to mirror
 * the screen depends on that identity holding.
 *
 * A filter at its unfiltered value is OMITTED rather than sent as `false`, so
 * the ordinary view has a clean, short, shareable URL and `?` means "the whole
 * roster" in exactly one way.
 */
function appendHolderListFilters(params: URLSearchParams, filters: HolderListFilters): void {
  if (filters.outstanding) params.set("outstanding", "true");
  if (filters.ticketTypeId) params.set("ticket_type_id", filters.ticketTypeId);
  if (filters.channel) params.set("channel", filters.channel);
  if (filters.soldFrom) params.set("sold_from", filters.soldFrom);
  if (filters.soldTo) params.set("sold_to", filters.soldTo);
}

/**
 * Writes sort/dir, omitting BOTH when they match the default pair — the
 * ordinary view carries no sort in its URL at all. They are written together
 * because half a sort is not a sort: a `dir` with no `sort` would be read
 * against whatever the default field becomes later.
 */
function appendHolderSort(params: URLSearchParams, sort: HolderSortField, dir: HolderSortDir): void {
  if (sort !== DEFAULT_HOLDER_SORT || dir !== DEFAULT_HOLDER_DIR) {
    params.set("sort", sort);
    params.set("dir", dir);
  }
}

/**
 * The canonical query string for the Holder List's URL: a leading `?`, or the
 * empty string when there is nothing to say.
 *
 * Page 1 is omitted, as are default sort and unset filters, so the plain roster
 * is `/events/:id/sales/holders` and not `?page=1&dir=asc` — a link somebody
 * would hesitate to paste. ONE BUILDER for the address bar and the fetch, for
 * `appendHolderListFilters`'s reason.
 */
export function holderListQuery(
  page: number,
  filters: HolderListFilters,
  sort: HolderSortField = DEFAULT_HOLDER_SORT,
  dir: HolderSortDir = DEFAULT_HOLDER_DIR,
): string {
  const params = new URLSearchParams();
  if (page > 1) params.set("page", String(page));
  appendHolderListFilters(params, filters);
  appendHolderSort(params, sort, dir);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/**
 * Whether the view is narrowed at all — what the Clear control's presence and
 * the wording of the empty state both hang on.
 *
 * The SORT IS NOT A FILTER and is deliberately not counted: reordering hides
 * nobody, and offering to "clear" it would suggest rows are missing when none
 * are.
 */
export function hasActiveHolderListFilters(filters: HolderListFilters): boolean {
  return (
    filters.outstanding ||
    filters.ticketTypeId !== "" ||
    filters.channel !== "" ||
    filters.soldFrom !== "" ||
    filters.soldTo !== ""
  );
}

/**
 * Whether `outstanding` is the ONLY thing narrowing this view.
 *
 * The empty state hangs on it (ADR 0065). An empty Outstanding-only view is a
 * CONGRATULATION — every required question has been answered on every live
 * Ticket. Under any other filter an empty view means "nothing matched", and the
 * two must not share a sentence: telling an Organizer who filtered to VIP door
 * sales in January that every question is answered would be false about the
 * Event, and they would stop chasing.
 */
export function isOutstandingTheOnlyFilter(filters: HolderListFilters): boolean {
  return (
    filters.outstanding &&
    filters.ticketTypeId === "" &&
    filters.channel === "" &&
    filters.soldFrom === "" &&
    filters.soldTo === ""
  );
}

/**
 * Reads a page of the Event's Holder List for the view the URL describes.
 *
 * The filters and the non-default sort are appended by the SAME helpers the URL
 * builder uses, so what is fetched is what the address bar says — the property
 * the Holder Export's claim to mirror the screen rests on.
 */
export async function fetchHolderList(
  eventId: string,
  page: number,
  filters: HolderListFilters,
  sort: HolderSortField = DEFAULT_HOLDER_SORT,
  dir: HolderSortDir = DEFAULT_HOLDER_DIR,
): Promise<HolderListPage> {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(HOLDER_LIST_PAGE_SIZE),
  });
  appendHolderListFilters(query, filters);
  appendHolderSort(query, sort, dir);
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
