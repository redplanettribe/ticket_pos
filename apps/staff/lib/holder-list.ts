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

import { ApiError, fetchEventsJSON } from "./events-api.ts";
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
  /*
    THE DOSSIER LINKS (#640). The buyer's Customer id, always; and the Holder's,
    only once the assignment is ACCEPTED — an address nobody accepted is not a
    Customer this Event may open a Dossier on. See `holderDossierCustomerId`.
  */
  customer_id: string;
  holder_customer_id?: string;
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
  since #526 a customer's email address REACHES browser history and any pasted
  link. That is accepted ONLY because the Sales list already does exactly this;
  tightening it is one change across both screens, not a special case here, and
  is out of scope of this module. What the search may match at all is a
  different and stricter question, decided in the API and described on `q`
  below: searchable if and only if displayable.

  ONLY THE FILTERS THAT EXIST TODAY LIVE HERE. #523 added the three structural
  ones — Ticket Type, Sales Channel and the sale's date range — #524 the
  assignment state, #525 the named question and #526 the search, leaving the
  five sorts in #527. The types below are shaped so those tickets add fields and
  values without restructuring anything, and #523 through #526 are the evidence
  that they do.
*/

/**
 * The Holder List's filters, mirroring the endpoint's query params, one field
 * per dimension.
 *
 * A RECORD AND NOT A BAG OF OPTIONALS, so the sort #527 adds is a compile error
 * everywhere it must be handled rather than a silently-absent key:
 * `EMPTY_HOLDER_LIST_FILTERS`, the append helper and the two active checks are
 * each total over this type. #523 added three fields under exactly that
 * property, #524 a fourth, #525 a fifth and #526 a sixth, and the compiler
 * named every place each of them had to be handled.
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
   * The search box: one case-insensitive substring over the BUYER's name and
   * email address, the Sale Confirmation reference, and — for an ACCEPTED
   * Holder only — that Holder's name and email address (#526).
   *
   * SEARCHABLE IF AND ONLY IF DISPLAYABLE, and the rule is the API's (ADR
   * 0065). An address a buyer typed into a Ticket Assignment and whose owner
   * never accepted matches NOTHING, and neither does a purged one: it is named
   * nowhere on this platform (ADR 0047), and a search that found it would
   * confirm one address at a time that its owner is on this roster. NOTHING ON
   * THIS SIDE ENFORCES THAT and nothing on this side may appear to — the
   * condition lives inside the API's search predicate, and a second opinion
   * here would be the copy that drifts.
   *
   * The accepted cost, so the control's copy stays honest: an Organizer who
   * typed an address into an assignment cannot search for it and must find the
   * row by its buyer or its reference. It is why the placeholder promises a
   * buyer, a reference and an ACCEPTED holder, and never holder addresses
   * generally.
   *
   * IT BELONGS TO NO FEATURE FLAG, alone among these filters. A buyer's name, a
   * buyer's address and a Sale Confirmation reference are on every roster of
   * every build, so the control is drawn on a plain roster with both features
   * dark — where the Holder branch of the API's predicate simply never matches,
   * because nobody has ever accepted anything.
   *
   * IT REACHES THE URL, and therefore browser history and any pasted link. That
   * is accepted only because the Sales list already does exactly this;
   * tightening it is one change across both screens, not a special case here.
   */
  q: string;
  /**
   * ONE NAMED Ticket Question's debtors (#525): "who still hasn't told me their
   * shirt size", which on an Event asking several questions is a different
   * chase from "who owes anything at all".
   *
   * IT COMPOSES WITH `outstanding` AND DOES NOT REPLACE IT. Both set means what
   * this alone means — owing this question implies owing something — and the
   * checkbox is untouched.
   *
   * NOTHING ON THIS SIDE DECIDES WHAT IS OUTSTANDING, which is this whole
   * module's first paragraph and matters here more than anywhere: the API
   * narrows its own derivation by the question's id, so a retired question, an
   * optional one and a reversed sale answer to this filter exactly as they
   * answer to `outstanding`. Naming a question nobody owes returns an empty
   * roster, which is the truthful answer.
   *
   * IT BELONGS TO TICKET QUESTIONS and the API IGNORES it while that feature is
   * dark, answering with the whole roster rather than a refusal — so a stale
   * bookmark keeps working and nothing here has to know a flag it cannot see
   * (ADR 0045). The CONTROL is drawn only where the payload proves the
   * questions side of the list exists; see `questionsVisible`.
   */
  questionId: string;
  /**
   * Where a Ticket stands with its Holder, in FOUR values over three states
   * (#524): `unassigned`, `assigned`, `accepted` — and `never_accepted`, "who
   * did I name who never claimed their ticket", which is the morning-after
   * question.
   *
   * `never_accepted` IS A VALUE OF THIS FILTER AND NOT A FOURTH STATE.
   * `TicketAssignmentState` below still has three members and #331 rejected a
   * fourth; the API derives the marker at read time from the retention purge
   * and this filter selects the rows that carry it. The four values are
   * `HOLDER_LIST_ASSIGNMENT_STATES` below, a union of its own — widening
   * `TicketAssignmentState` to fit the control is the mistake that union's
   * comment exists to prevent.
   *
   * A STRING AND NOT THAT UNION, exactly as `channel` is a string: the URL is
   * the source of truth and is taken as it comes, unvalidated on this side, so
   * one opinion about which values are usable lives in the API and not two.
   *
   * IT BELONGS TO TICKET ASSIGNMENT and the API IGNORES it while that feature
   * is dark, answering with the whole roster rather than a refusal — so nothing
   * on this side has to know a flag it cannot see (ADR 0045), and a stale
   * bookmark carrying it keeps working. The CONTROL is drawn only when the
   * payload proves the feature exists; see `holderListVisible`.
   */
  assignmentState: string;
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
  q: "",
  questionId: "",
  assignmentState: "",
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
 * FIVE FIELDS (#527, ADR 0065), each working in both directions:
 *
 * - `sold_at` — when the sale happened, and THE DEFAULT.
 * - `buyer` — the buyer's name, last then first, as the Sales list's `customer`
 *   sort reads it, so a roster and a ledger order people the same way.
 * - `holder` — who is coming, from the accepted Holder's own name. ROWS WITH NO
 *   HOLDER NAME COME LAST IN BOTH DIRECTIONS, which is the API's rule and is
 *   argued there: on an Event where most Tickets are unassigned the conventional
 *   flip would open one of the two directions on hundreds of blank cells.
 * - `ticket_type` — the Event's own CATALOG DISPLAY ORDER and not alphabetical,
 *   so Early Bird / General / VIP stays the order the Organization chose.
 * - `owes` — how many required Answers a Ticket still owes, worst first, so the
 *   chase has somewhere to start. It BELONGS TO TICKET QUESTIONS: the API
 *   ignores it while that feature is dark and answers in the default order, and
 *   the header offering it is drawn only under `showQuestions`, so nothing on
 *   this side has to know a flag it cannot see.
 */
export type HolderSortField = "sold_at" | "buyer" | "holder" | "ticket_type" | "owes";
export type HolderSortDir = "asc" | "desc";

export const HOLDER_SORT_FIELDS: readonly HolderSortField[] = [
  "sold_at",
  "buyer",
  "holder",
  "ticket_type",
  "owes",
];

export const DEFAULT_HOLDER_SORT: HolderSortField = "sold_at";

/**
 * The direction a newly chosen column starts in, the way the Sales list's
 * `defaultDirFor` does — clicking a column should show the useful end of it
 * first, and only a second click asks for the other.
 *
 * NAMES ASCEND AND THE DEBT DESCENDS. A→Z is how a person looks somebody up in
 * a list of names, which is `buyer` and `holder`; `sold_at` opens oldest-first
 * because that is this roster's own default and a column that jumped to newest
 * on its first click would contradict the page it is on; `ticket_type` ascends
 * into the catalog's own order, which is the order the Organization wrote it in.
 * `owes` DESCENDS, and that is the whole point of it: the Tickets owing most are
 * the ones somebody opened this sort to chase, and starting at zero would put
 * every Ticket that owes nothing in front of them.
 */
export function defaultHolderDirFor(field: HolderSortField): HolderSortDir {
  return field === "owes" ? "desc" : "asc";
}

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
  // `q` first, as it is first in the filter bar and first on the Sales list's
  // URL — a narrowed view reads with the person being looked for at the front.
  // Named as the API names it, and OMITTED when empty like every other filter,
  // so the plain roster's URL says nothing about a search and a shared link
  // carries no stray customer address.
  if (filters.q) params.set("q", filters.q);
  if (filters.outstanding) params.set("outstanding", "true");
  // Beside `outstanding`, because it is the other half of the same question:
  // the boolean and the named question narrow the same debt, and a reader of a
  // pasted URL should meet them together.
  if (filters.questionId) params.set("question_id", filters.questionId);
  if (filters.assignmentState) params.set("assignment_state", filters.assignmentState);
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
    filters.q !== "" ||
    filters.questionId !== "" ||
    filters.assignmentState !== "" ||
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
    filters.q === "" &&
    filters.questionId === "" &&
    filters.assignmentState === "" &&
    filters.ticketTypeId === "" &&
    filters.channel === "" &&
    filters.soldFrom === "" &&
    filters.soldTo === ""
  );
}

/**
 * The three sentences an EMPTY Holder List can say, as catalog keys in the
 * `outstandingAnswers` namespace.
 *
 * A union of literal keys and not a `string`, so a key removed from the catalog
 * is a compile error at the call site rather than a blank paragraph on screen.
 */
export type HolderListEmptyStateKey = "nothingOutstanding" | "noMatchingTickets" | "noTickets";

/**
 * Which sentence an empty Holder List says (#528, ADR 0065).
 *
 * THREE FACTS, THREE SENTENCES, and the selection lives here rather than as
 * ternaries in the component because it is the one rule on this screen that can
 * be wrong in a way nobody sees: every branch renders a plausible grey
 * paragraph, and only the fact behind it differs.
 *
 * THE CONGRATULATION IS NARROWER THAN THE FILTER. "Every required question has
 * been answered on every live Ticket" is a claim about THE WHOLE EVENT, and an
 * empty view supports it only when `outstanding` is the sole narrowing. Under a
 * Ticket Type, a channel, a date range or a search, an empty view means "nothing
 * matched here" — "no VIP owes anything" is true and is not the same sentence,
 * and an Organizer told the second when the first is what happened stops
 * chasing debts that are still owed.
 *
 * An empty roster under NO filter is neither: nothing has been sold yet.
 *
 * THE SORT IS NOT A FILTER and is deliberately not an argument. Reordering
 * narrows nothing, so a reader who sorted the Outstanding-only view by name
 * still earns the congratulation; taking `sort` here would be an invitation to
 * count it.
 */
export function holderListEmptyStateKey(filters: HolderListFilters): HolderListEmptyStateKey {
  if (isOutstandingTheOnlyFilter(filters)) return "nothingOutstanding";
  if (hasActiveHolderListFilters(filters)) return "noMatchingTickets";
  return "noTickets";
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
 * The BFF path for the Holder Export under the given filters and sort (#529,
 * ADR 0065).
 *
 * IT IS BUILT FROM THE SAME APPEND HELPERS THE URL AND THE FETCH USE, and that
 * identity is the whole basis of the file's claim to mirror the screen. A path
 * assembled by hand here could drift by one parameter — a filter forgotten, a
 * sort spelled differently — and the file would come back narrower or wider than
 * the roster the reader was looking at, with nothing on either surface saying
 * so.
 *
 * PAGINATION IS DELIBERATELY ABSENT: the file is the whole answer, not a page of
 * it, and the API ignores the page parameters regardless.
 */
export function holderExportPath(
  eventId: string,
  filters: HolderListFilters,
  sort: HolderSortField = DEFAULT_HOLDER_SORT,
  dir: HolderSortDir = DEFAULT_HOLDER_DIR,
): string {
  const params = new URLSearchParams();
  appendHolderListFilters(params, filters);
  appendHolderSort(params, sort, dir);
  const query = params.toString();
  return `/api/events/${eventId}/holder-list/export${query ? `?${query}` : ""}`;
}

/**
 * Reads the download's filename out of a Content-Disposition header. The API
 * decides the name so it is decided in one place; this only reads it back,
 * falling back to a plain name if the header is missing.
 */
function holderExportFilenameFrom(disposition: string | null): string {
  const match = disposition?.match(/filename="?([^"]+)"?/);
  return match?.[1] ?? "holder-export.xlsx";
}

/**
 * Fetches the Holder Export and saves it as a file.
 *
 * IT FETCHES A BLOB RATHER THAN NAVIGATING TO THE URL, and that is required
 * rather than cosmetic: the endpoint returns a FILE on success and a JSON error
 * envelope on failure, so a plain link would send the browser to raw JSON
 * whenever the export was refused. Fetching also lets the caller show a spinner
 * while a large roster streams in.
 *
 * AND READING THE WHOLE BLOB BEFORE SAVING IS WHAT MAKES A CUT DOWNLOAD SAVE
 * NOTHING (ADR 0075). The export streams with no size limit, so a failure part
 * way arrives as a broken connection rather than an envelope, and `blob()`
 * rejects on it. That rejection propagates from here before any link exists, so
 * no file is saved and the caller shows a failed download - a roster missing
 * its last people can never reach anybody's Downloads folder.
 *
 * The envelope is re-thrown WHOLE — message, code and details — rather than
 * pre-worded here. Choosing the sentence is the call site's job: it reads the
 * catalog by code, then its own copy, and neither answer can be spelled in lib/.
 */
export async function downloadHolderExport(
  eventId: string,
  filters: HolderListFilters,
  sort: HolderSortField = DEFAULT_HOLDER_SORT,
  dir: HolderSortDir = DEFAULT_HOLDER_DIR,
): Promise<void> {
  const response = await fetch(holderExportPath(eventId, filters, sort, dir));
  if (!response.ok) {
    const envelope = (await response.json().catch(() => null)) as {
      error?: { message?: string; code?: string; details?: unknown };
    } | null;
    throw new ApiError(
      envelope?.error?.message ?? "",
      envelope?.error?.code,
      envelope?.error?.details as Record<string, unknown> | undefined,
    );
  }

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = holderExportFilenameFrom(response.headers.get("Content-Disposition"));
    document.body.appendChild(link);
    link.click();
    link.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
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
 * What the assignment-state FILTER may select: the three states, plus
 * `never_accepted` (#524).
 *
 * ITS OWN UNION, AND NOT A WIDENING OF `TicketAssignmentState`. That type has
 * three members because the API has three states and #331 rejected a fourth;
 * adding `never_accepted` to it here would make every row's `assignment_state`
 * appear able to arrive as a value the API never sends, and would put a fourth
 * state on this side of the wire — which is the exact mistake the whole
 * arrangement is built to avoid. A filter is a QUESTION, and a question may name
 * a fact that is not a state.
 */
export type HolderStateValue = TicketAssignmentState | "never_accepted";

/**
 * The four values in the order the control offers them: the roster's ordinary
 * case first, then the two live ones in the order a Ticket passes through them,
 * and the ending last.
 */
export const HOLDER_LIST_ASSIGNMENT_STATES: readonly HolderStateValue[] = [
  "unassigned",
  "assigned",
  "accepted",
  "never_accepted",
];

/**
 * The catalog key each filter value is named with — the SAME words the rows are
 * already labelled with, reused rather than re-coined.
 *
 * Coining a second word for "never accepted" is how a filter comes to promise
 * one thing and the rows beneath it to say another, in one language and not the
 * other. Built from `ASSIGNMENT_STATE_KEYS` and `holderStateKey`'s own extra
 * key, and total over the four values, so a fifth could not reach the control
 * as a blank option.
 */
export const HOLDER_STATE_VALUE_KEYS = {
  ...ASSIGNMENT_STATE_KEYS,
  never_accepted: "holderNeverAccepted",
} as const satisfies Record<HolderStateValue, HolderStateKey>;

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
 * Whether to draw the assignment-state FILTER control.
 *
 * NOT THE SAME QUESTION AS `holderListVisible`, and conflating the two was a
 * bug. That predicate asks whether the assignment side of the list EXISTS,
 * which it answers off the payload's absences (ADR 0045) — the only honest
 * source, since this app holds no copy of a deployment flag. But it reads the
 * CURRENT PAGE'S ROWS, and an empty result page is not a payload absence: it is
 * an empty result. Filter to `assignment_state=accepted` on an Event where
 * nobody has accepted and every row disappears, taking with it the very control
 * that produced the view — leaving the reader a Clear button and no way to see
 * what they had asked for.
 *
 * SO AN ACTIVE NARROWING ON AN EMPTY PAGE KEEPS THE CONTROL, which is the
 * Outstanding checkbox's own rule and the filter bar's: a control stays drawn
 * while its own filtered view is empty, because changing it is the way back and
 * a bar that vanished with the last row would strand the reader.
 *
 * THE DARK CASE IS STILL SAFE, AND THE ORDER OF THE TWO TESTS IS WHY. With rows
 * on the page the answer is the payload's, unchanged: a dark build omits
 * `assignment_state` from every row, so the control is not drawn however the URL
 * is written. The fallback can only fire on an EMPTY page, where there is no
 * absence to read and no evidence either way — and there the reader who is
 * plainly holding this filter is served better by a control they can clear than
 * by a screen that swallowed it. On a dark build that costs a hand-crafted URL a
 * select whose values the API ignores; on an open build it costs nothing and
 * fixes the stranding.
 */
export function assignmentStateFilterVisible(
  rows: readonly HolderTicket[],
  assignmentState: string,
): boolean {
  if (holderListVisible(rows)) {
    return true;
  }
  return rows.length === 0 && assignmentState !== "";
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

/**
 * The Customer whose Dossier a row's Holder name opens (#640), or null when
 * the name is not a link.
 *
 * ONLY AN ACCEPTED ASSIGNMENT. The API sends `holder_customer_id` only then, and
 * this checks the state as well, so an unaccepted or purged row can never offer
 * a Dossier on somebody who never said yes — whatever a payload carries.
 */
export function holderDossierCustomerId(
  ticket: Pick<HolderTicket, "assignment_state" | "holder_customer_id">,
): string | null {
  if (ticket.assignment_state !== "accepted" || !ticket.holder_customer_id) {
    return null;
  }
  return ticket.holder_customer_id;
}
