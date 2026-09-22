import assert from "node:assert/strict";
import { test } from "node:test";

import {
  ASSIGNMENT_STATE_KEYS,
  HOLDER_LIST_ASSIGNMENT_STATES,
  HOLDER_LIST_CHANNELS,
  HOLDER_SORT_FIELDS,
  HOLDER_STATE_VALUE_KEYS,
  DEFAULT_HOLDER_DIR,
  DEFAULT_HOLDER_SORT,
  EMPTY_HOLDER_LIST_FILTERS,
  SALES_CHANNEL_KEYS,
  assignmentStateBadgeVariant,
  assignmentStateFilterVisible,
  buyerName,
  defaultHolderDirFor,
  downloadHolderExport,
  hasActiveHolderListFilters,
  holderBadgeVariant,
  holderListEmptyStateKey,
  holderExportPath,
  holderListQuery,
  holderListVisible,
  holderName,
  holderDossierCustomerId,
  holderStateKey,
  isOutstandingTheOnlyFilter,
  parseHolderDir,
  parseHolderSort,
  questionsVisible,
} from "./holder-list.ts";
import type { HolderListFilters, HolderListPage, HolderTicket } from "./holder-list.ts";

const ticket = (first: string, last: string): HolderTicket => ({
  ticket_id: "tk_1",
  ordinal: 1,
  ticket_type_id: "tt_1",
  ticket_type_name: "General",
  ticket_sale_id: "s_1",
  confirmation_ref: "ABC-123",
  channel: "online",
  customer_id: "cus_buyer",
  customer_first_name: first,
  customer_last_name: last,
  customer_email: "ana@example.com",
  sold_at: "2026-07-01T10:00:00Z",
  outstanding: [],
});

test("a buyer's name is the two halves joined", () => {
  assert.equal(buyerName(ticket("Ana", "López")), "Ana López");
});

// Half a name is still a name, and a stray space is not. The API keeps the two
// parts apart and either can be blank on a sale recorded by hand.
test("a missing half leaves no stray space", () => {
  assert.equal(buyerName(ticket("Ana", "")), "Ana");
  assert.equal(buyerName(ticket("", "López")), "López");
  assert.equal(buyerName(ticket("  ", "  ")), "");
});

// The three Sales Channels are named from the SALES catalog and never re-coined
// here: a channel translated per screen is how there come to be two Spanish
// words for a door sale. This asserts the mapping is total, so a fourth channel
// could not be drawn as a blank cell.
test("every Sales Channel has a key, and they are the sales catalog's", () => {
  assert.deepEqual(SALES_CHANNEL_KEYS, {
    online: "channelOnline",
    in_person: "channelInPerson",
    import: "channelImport",
  });
});

// THE HOLDER LIST (#333, #329, ADR 0047). Every Ticket of the Event, who is
// coming on each, and what it still owes. Nothing here decides a state — the
// API derives it from three columns so that the buyer's page, this list and
// the export cannot disagree — and these tests hold the shaping, which is all
// this module does.

const holderRow = (fields: Partial<HolderTicket>): HolderTicket => ({
  ...ticket("Ana", "López"),
  ...fields,
});

test("an accepted Holder's name is the two halves joined", () => {
  assert.equal(
    holderName(holderRow({ holder_first_name: "Carla", holder_last_name: "Ruiz" })),
    "Carla Ruiz",
  );
});

// A row can carry a state and no Holder at all, which is the `assigned` case:
// an address was typed, nobody accepted it, and the platform tells the
// Organization the state and never the person.
test("a row with no Holder yields no name and no stray space", () => {
  assert.equal(holderName(holderRow({ assignment_state: "assigned" })), "");
  assert.equal(holderName(holderRow({ holder_first_name: " ", holder_last_name: "" })), "");
});

// Total over the three states, so a fourth arriving from the API could not be
// drawn as a blank cell.
test("every assignment state has a key", () => {
  assert.deepEqual(ASSIGNMENT_STATE_KEYS, {
    unassigned: "holderUnassigned",
    assigned: "holderAssigned",
    accepted: "holderAccepted",
  });
});

// A PURGED ROW READS "ASSIGNED, NEVER ACCEPTED" (#334). The API sends it as
// `assigned` with `never_accepted` beside it — derived at read time from the
// purge marker, never a fourth state — and this is the ONE place the marker
// changes a word, so the morning-after sheet can tell "nobody was named" from
// "named and never claimed".
test("a never-accepted row takes its own key; every other row takes its state's", () => {
  assert.equal(
    holderStateKey(holderRow({ assignment_state: "assigned", never_accepted: true })),
    "holderNeverAccepted",
  );
  assert.equal(holderStateKey(holderRow({ assignment_state: "assigned" })), "holderAssigned");
  assert.equal(holderStateKey(holderRow({ assignment_state: "accepted" })), "holderAccepted");
  assert.equal(holderStateKey(holderRow({ assignment_state: "unassigned" })), "holderUnassigned");
});

test("the state decides the badge, in one place", () => {
  assert.equal(assignmentStateBadgeVariant("accepted"), "success");
  assert.equal(assignmentStateBadgeVariant("assigned"), "warning");
  assert.equal(assignmentStateBadgeVariant("unassigned"), "outline");
});

// A never-accepted row is drawn QUIETLY, not as a warning: it is history, and
// nothing anybody does now brings that person. `assigned` stays a warning
// because it is still actionable.
test("a never-accepted row is drawn quietly; a waiting one is not", () => {
  assert.equal(
    holderBadgeVariant(holderRow({ assignment_state: "assigned", never_accepted: true })),
    "outline",
  );
  assert.equal(holderBadgeVariant(holderRow({ assignment_state: "assigned" })), "warning");
  assert.equal(holderBadgeVariant(holderRow({ assignment_state: "accepted" })), "success");
});

// THE FEATURE FLAGS ARE THE PAYLOAD'S ABSENCES AND NOTHING ELSE. With
// TICKET_ASSIGNMENT_ENABLED closed the API omits every assignment field, so this
// app holds no second copy of a deployment flag it cannot see (ADR 0045) — and
// the Holder column disappears rather than filling a screen with a word nobody
// can act on.
test("the Holder column is hidden when no row carries an assignment", () => {
  assert.equal(holderListVisible([]), false);
  assert.equal(holderListVisible([ticket("Ana", "López")]), false);
  assert.equal(holderListVisible([holderRow({ assignment_state: "unassigned" })]), true);
});

// THE CONTROL THAT PRODUCED THE VIEW MUST SURVIVE THE VIEW BEING EMPTY.
//
// `holderListVisible` reads the CURRENT PAGE'S rows, which is the right reading
// for a COLUMN and the wrong one for the FILTER: narrow to `accepted` on an
// Event where nobody has accepted and the page is empty, so the select that set
// the filter disappeared and left only Clear. An empty RESULT is not a payload
// ABSENCE, and this is where the two are told apart — the same rule the
// Outstanding checkbox has always had, since changing the control is the way
// back.
//
// The dark build is unaffected because the absence reading wins wherever there
// are rows to read it from: only an EMPTY page reaches the fallback, and only
// with the filter actually set.
test("the assignment-state filter survives its own empty result", () => {
  const assigned = [holderRow({ assignment_state: "assigned" })];
  const dark = [ticket("Ana", "López")];

  // No filter set: the control follows the payload exactly as before.
  assert.equal(assignmentStateFilterVisible([], ""), false);
  assert.equal(assignmentStateFilterVisible(dark, ""), false);
  assert.equal(assignmentStateFilterVisible(assigned, ""), true);

  // Filtered to a state nobody is in: the page is empty and the control STAYS,
  // because unsetting it is the only way back.
  assert.equal(assignmentStateFilterVisible([], "accepted"), true);

  // Filtered on an open build that did match: unchanged, the payload says so.
  assert.equal(assignmentStateFilterVisible(assigned, "assigned"), true);

  // A dark build still hides it whenever there are rows, however the URL reads —
  // the API omits `assignment_state` from every one of them.
  assert.equal(assignmentStateFilterVisible(dark, "accepted"), false);
});

// And the questions side — the Owes column, the debt summary, the Outstanding
// Answers filter — is read off the OTHER flag's absence: `outstanding_count`
// is omitted while TICKET_QUESTIONS_ENABLED is closed, and present (even at 0)
// while it is open. The Holder List works as a plain roster without it (#333).
test("the questions side follows outstanding_count's presence", () => {
  const page = (outstanding_count?: number): HolderListPage => ({
    data: [],
    pagination: { page: 1, page_size: 50, total: 0, total_pages: 0 },
    ...(outstanding_count === undefined ? {} : { outstanding_count }),
  });
  assert.equal(questionsVisible(page()), false);
  assert.equal(questionsVisible(page(0)), true);
  assert.equal(questionsVisible(page(7)), true);
});

// --- the view in the URL (#522, ADR 0065) ---------------------------------

/*
  These hold the property the Holder Export's whole claim rests on: the query
  string is built from the filter set, and the same builder feeds the fetch, so
  the address bar and the file cannot describe different rosters.

  The default view producing an EMPTY query is the load-bearing case. It is what
  makes the plain roster a link somebody would paste, and it is also the proof
  that this change is invisible to a reader who touches no control — no query
  string means the page fetches exactly page 1 of the whole roster, as it did
  when the filter was component state.
*/

const filters = (fields: Partial<HolderListFilters> = {}): HolderListFilters => ({
  ...EMPTY_HOLDER_LIST_FILTERS,
  ...fields,
});

test("the default view has no query string at all", () => {
  assert.equal(holderListQuery(1, filters()), "");
  // And the default sort is spelled out rather than left implicit: naming it
  // must not change the URL, or every bookmark of the plain roster grows a
  // sort it never asked for.
  assert.equal(holderListQuery(1, filters(), DEFAULT_HOLDER_SORT, DEFAULT_HOLDER_DIR), "");
});

test("page 1 is omitted; every later page is named", () => {
  assert.equal(holderListQuery(1, filters({ outstanding: true })), "?outstanding=true");
  assert.equal(holderListQuery(3, filters()), "?page=3");
  assert.equal(holderListQuery(3, filters({ outstanding: true })), "?page=3&outstanding=true");
});

// An unfiltered dimension is ABSENT, never `outstanding=false`: the roster's
// URL says one thing in one way, and a reader pasting it gets the plain list.
test("a filter at its unfiltered value is omitted, not sent as false", () => {
  assert.equal(holderListQuery(1, filters({ outstanding: false })), "");
});

// A non-default sort carries BOTH halves. Half a sort is not a sort: a `dir`
// with no `sort` would be read against whatever the default field is at the
// time, which is a URL that means something different after the next change.
test("a non-default sort is carried whole", () => {
  assert.equal(holderListQuery(1, filters(), "sold_at", "desc"), "?sort=sold_at&dir=desc");
  assert.equal(holderListQuery(1, filters(), "holder", "asc"), "?sort=holder&dir=asc");
});

// THE FIVE, AND EXACTLY THE FIVE (#527). The allowlist mirrors the API's own,
// which mirrors the repository's — an unrecognised key must never reach a query,
// and a key this side offered that the API did not know would be a header that
// silently does nothing.
test("the roster sorts five ways", () => {
  assert.deepEqual(HOLDER_SORT_FIELDS, ["sold_at", "buyer", "holder", "ticket_type", "owes"]);
});

// NAMES ASCEND AND THE DEBT DESCENDS. Clicking a column shows the useful end of
// it first: A→Z for the two name columns, oldest-first for the sale date because
// that is this roster's own default, the catalog's own order for Ticket Type —
// and MOST OWED FIRST for `owes`, which is the entire reason somebody reaches
// for that column. Starting it at zero would put every Ticket owing nothing in
// front of the ones being chased.
test("a new sort column starts in its own natural direction", () => {
  assert.equal(defaultHolderDirFor("owes"), "desc");
  for (const field of ["sold_at", "buyer", "holder", "ticket_type"] as const) {
    assert.equal(defaultHolderDirFor(field), "asc");
  }
});

// Every allowlisted field survives a round trip through the URL, so a header
// added to the table cannot become a link that quietly reverts to sold_at.
test("every allowlisted sort survives the URL", () => {
  for (const field of HOLDER_SORT_FIELDS) {
    assert.equal(parseHolderSort(field), field);
    assert.equal(
      holderListQuery(1, filters(), field, "desc"),
      `?sort=${field}&dir=desc`,
    );
  }
});

// OLDEST SALE FIRST, and deliberately not the Sales list's newest-first: the
// roster is worked through from the top, and #527 names keeping this order as
// its own criterion. Flipping it silently re-orders every existing bookmark.
test("the default sort is the oldest sale first", () => {
  assert.equal(DEFAULT_HOLDER_SORT, "sold_at");
  assert.equal(DEFAULT_HOLDER_DIR, "asc");
});

// A hand-edited or stale URL stays usable rather than erroring — the API
// re-validates regardless, and this is the surface, not the boundary.
test("an unknown sort or direction falls back to the default", () => {
  assert.equal(parseHolderSort("sold_at"), "sold_at");
  // `amount` is the Sales list's and is not on this roster — money is not a
  // column here. A URL borrowed from that screen is a roster, not an error.
  assert.equal(parseHolderSort("amount"), DEFAULT_HOLDER_SORT);
  assert.equal(parseHolderSort("holder_email"), DEFAULT_HOLDER_SORT);
  assert.equal(parseHolderSort(undefined), DEFAULT_HOLDER_SORT);
  assert.equal(parseHolderDir("desc"), "desc");
  assert.equal(parseHolderDir("sideways"), DEFAULT_HOLDER_DIR);
  assert.equal(parseHolderDir(undefined), DEFAULT_HOLDER_DIR);
});

// What the Clear control's presence hangs on. The SORT IS NOT A FILTER and is
// deliberately not counted: reordering hides nobody, and offering to clear it
// would suggest rows are missing when none are.
test("the whole roster has no active filters; a narrowing does", () => {
  assert.equal(hasActiveHolderListFilters(EMPTY_HOLDER_LIST_FILTERS), false);
  assert.equal(hasActiveHolderListFilters(filters({ outstanding: true })), true);
});

/*
  THE STRUCTURAL FILTERS (#523): Ticket Type, Sales Channel and the sale's date
  range. They compose with `outstanding` and with each other, and every one of
  them reaches the URL under the API's OWN param name — the property the Holder
  Export's "this list, as you are looking at it" depends on.
*/

test("each structural filter reaches the URL under the API's name", () => {
  assert.equal(holderListQuery(1, filters({ ticketTypeId: "tt_vip" })), "?ticket_type_id=tt_vip");
  assert.equal(holderListQuery(1, filters({ channel: "in_person" })), "?channel=in_person");
  assert.equal(holderListQuery(1, filters({ soldFrom: "2026-07-01" })), "?sold_from=2026-07-01");
  assert.equal(holderListQuery(1, filters({ soldTo: "2026-07-31" })), "?sold_to=2026-07-31");
});

// They COMPOSE, which is the ticket's whole claim: "which VIP door sales are
// still unclaimed, sold in July" is one address.
test("the filters compose into one query, page included", () => {
  assert.equal(
    holderListQuery(
      2,
      filters({
        outstanding: true,
        ticketTypeId: "tt_vip",
        channel: "in_person",
        soldFrom: "2026-07-01",
        soldTo: "2026-07-31",
      }),
    ),
    "?page=2&outstanding=true&ticket_type_id=tt_vip&channel=in_person&sold_from=2026-07-01&sold_to=2026-07-31",
  );
});

// A blank string is the UNFILTERED value and is omitted, exactly as
// `outstanding: false` is — so a reader who opened a date picker and closed it
// again is left with the roster's plain URL and not `?sold_from=`.
test("a blank structural filter is omitted, not sent empty", () => {
  assert.equal(
    holderListQuery(1, filters({ ticketTypeId: "", channel: "", soldFrom: "", soldTo: "" })),
    "",
  );
});

// The Clear control's presence, now that four more things can narrow the view.
// Each one alone is enough: a reader who set only a date must still be offered
// the way back.
test("any one structural filter makes the view narrowed", () => {
  assert.equal(hasActiveHolderListFilters(filters({ ticketTypeId: "tt_vip" })), true);
  assert.equal(hasActiveHolderListFilters(filters({ channel: "online" })), true);
  assert.equal(hasActiveHolderListFilters(filters({ soldFrom: "2026-07-01" })), true);
  assert.equal(hasActiveHolderListFilters(filters({ soldTo: "2026-07-31" })), true);
});

// THE CONGRATULATION IS NARROWER THAN THE FILTER (ADR 0065). An empty
// Outstanding-only view means every required question has been answered on
// every live Ticket; an empty view under ANY other filter means only that
// nothing matched. Telling an Organizer who filtered to VIP door sales that
// every question is answered would be false about the Event, and they would
// stop chasing.
test("outstanding is the sole filter only when nothing else narrows the view", () => {
  assert.equal(isOutstandingTheOnlyFilter(filters({ outstanding: true })), true);
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, ticketTypeId: "tt_vip" })),
    false,
  );
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, channel: "in_person" })),
    false,
  );
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, soldFrom: "2026-07-01" })),
    false,
  );
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, soldTo: "2026-07-31" })),
    false,
  );
  // And it is not the congratulation's cue when `outstanding` is off at all —
  // an empty unfiltered roster means nothing has been sold.
  assert.equal(isOutstandingTheOnlyFilter(EMPTY_HOLDER_LIST_FILTERS), false);
});

// The channel control offers exactly the three channels the API accepts, and
// every one of them has a word in the SALES catalog — so a fourth channel could
// not reach the dropdown as a blank option.
test("the channel filter offers every Sales Channel, each with a name", () => {
  assert.deepEqual([...HOLDER_LIST_CHANNELS], ["online", "in_person", "import"]);
  for (const channel of HOLDER_LIST_CHANNELS) {
    assert.ok(SALES_CHANNEL_KEYS[channel]);
  }
});

/*
  THE ASSIGNMENT-STATE FILTER (#524, ADR 0065): four selectable values over
  three states, `never_accepted` being a value of this filter and NOT a fourth
  assignment state. The API derives that marker at read time from the retention
  purge and #331 rejected making it a state; nothing on this side may widen
  `TicketAssignmentState` to accommodate the control.
*/

test("the assignment-state filter reaches the URL under the API's name", () => {
  assert.equal(
    holderListQuery(1, filters({ assignmentState: "never_accepted" })),
    "?assignment_state=never_accepted",
  );
  assert.equal(holderListQuery(1, filters({ assignmentState: "accepted" })), "?assignment_state=accepted");
});

// Blank is the unfiltered value and is omitted, as every other filter's is, so
// a reader who opened the control and put it back is left with the plain URL.
test("a blank assignment state is omitted, not sent empty", () => {
  assert.equal(holderListQuery(1, filters({ assignmentState: "" })), "");
});

// It composes with everything else, in the API's own param order — the
// property the Holder Export's "this list, as you are looking at it" rests on.
test("the assignment state composes with the other filters", () => {
  assert.equal(
    holderListQuery(
      2,
      filters({
        outstanding: true,
        assignmentState: "assigned",
        ticketTypeId: "tt_vip",
        channel: "in_person",
        soldFrom: "2026-07-01",
        soldTo: "2026-07-31",
      }),
    ),
    "?page=2&outstanding=true&assignment_state=assigned&ticket_type_id=tt_vip&channel=in_person" +
      "&sold_from=2026-07-01&sold_to=2026-07-31",
  );
});

// The Clear control's presence: a reader who narrowed by state alone must still
// be offered the way back.
test("an assignment state alone makes the view narrowed", () => {
  assert.equal(hasActiveHolderListFilters(filters({ assignmentState: "unassigned" })), true);
});

// And it is NOT the congratulation's cue. An empty "outstanding, never
// accepted" view means nothing matched that pair, not that every question on
// the Event has been answered.
test("outstanding is not the sole filter when a state narrows the view too", () => {
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, assignmentState: "never_accepted" })),
    false,
  );
});

// FOUR VALUES, AND THEY BORROW THE ROWS' OWN WORDS. Every value has a key, the
// three states take the ones the badges already use, and `never_accepted` takes
// `holderStateKey`'s own — coining a second word for it is how a filter comes
// to promise one thing and the rows beneath it to say another.
test("the state filter offers four values, each named as the rows name it", () => {
  assert.deepEqual(
    [...HOLDER_LIST_ASSIGNMENT_STATES],
    ["unassigned", "assigned", "accepted", "never_accepted"],
  );
  for (const state of HOLDER_LIST_ASSIGNMENT_STATES) {
    assert.ok(HOLDER_STATE_VALUE_KEYS[state]);
  }
  assert.equal(HOLDER_STATE_VALUE_KEYS.unassigned, ASSIGNMENT_STATE_KEYS.unassigned);
  assert.equal(HOLDER_STATE_VALUE_KEYS.assigned, ASSIGNMENT_STATE_KEYS.assigned);
  assert.equal(HOLDER_STATE_VALUE_KEYS.accepted, ASSIGNMENT_STATE_KEYS.accepted);
  assert.equal(HOLDER_STATE_VALUE_KEYS.never_accepted, "holderNeverAccepted");
  // AND THE STATE MAP STAYS AT THREE. The fourth value lives in the filter's
  // map alone; a `never_accepted` member appearing in ASSIGNMENT_STATE_KEYS is
  // the fourth state #331 rejected, arriving by the back door.
  assert.deepEqual(Object.keys(ASSIGNMENT_STATE_KEYS), ["unassigned", "assigned", "accepted"]);
});

/*
  THE NAMED-QUESTION FILTER (#525, ADR 0065): the Tickets owing ONE named
  Ticket Question, which on an Event asking several is a different chase from
  owing anything at all.

  NOTHING ON THIS SIDE DECIDES WHAT IS OUTSTANDING — the API narrows its own
  derivation by the question's id — so there is nothing here to test but the
  SHAPING: that the id reaches the URL under the API's own name, that it
  composes, and that it counts as a narrowing everywhere a narrowing counts.
*/

test("the named question reaches the URL under the API's name", () => {
  assert.equal(holderListQuery(1, filters({ questionId: "q_size" })), "?question_id=q_size");
});

// It carries the ID and not the label, which is why nothing here trims or
// cases it: a question's wording is the Organization's and can be corrected,
// and a URL keyed on words would stop meaning anything the day it was.
test("a blank named question is omitted, not sent empty", () => {
  assert.equal(holderListQuery(1, filters({ questionId: "" })), "");
});

// IT COMPOSES WITH `outstanding` RATHER THAN REPLACING IT, and both reach the
// URL together — the checkbox is untouched by this filter. The two mean the
// same view, because owing this question implies owing something, and the API
// is where that identity is proven; this only holds them to travelling
// together.
test("the named question composes with outstanding and with every other filter", () => {
  assert.equal(
    holderListQuery(1, filters({ outstanding: true, questionId: "q_size" })),
    "?outstanding=true&question_id=q_size",
  );
  assert.equal(
    holderListQuery(
      2,
      filters({
        outstanding: true,
        questionId: "q_size",
        assignmentState: "assigned",
        ticketTypeId: "tt_vip",
        channel: "in_person",
        soldFrom: "2026-07-01",
        soldTo: "2026-07-31",
      }),
    ),
    "?page=2&outstanding=true&question_id=q_size&assignment_state=assigned&ticket_type_id=tt_vip" +
      "&channel=in_person&sold_from=2026-07-01&sold_to=2026-07-31",
  );
});

// The Clear control's presence: a reader who narrowed to one question alone
// must still be offered the way back.
test("a named question alone makes the view narrowed", () => {
  assert.equal(hasActiveHolderListFilters(filters({ questionId: "q_size" })), true);
});

// AND IT IS NOT THE CONGRATULATION'S CUE. An empty "outstanding, shirt size"
// view means nobody owes a shirt size — not that every required question on the
// Event has been answered. Telling an Organizer the second when only the first
// is true is how they stop chasing the dietary notes.
test("outstanding is not the sole filter when a question narrows the view too", () => {
  assert.equal(
    isOutstandingTheOnlyFilter(filters({ outstanding: true, questionId: "q_size" })),
    false,
  );
});

/*
  THE SEARCH BOX (#526, ADR 0065): one person on the roster, by the buyer's name
  or address, by the Sale Confirmation reference, or by an ACCEPTED Holder's
  name or address.

  WHAT IS SEARCHABLE IS DECIDED IN THE API AND IS NOT TESTABLE FROM HERE, and
  that division is the point rather than a gap. The rule — searchable if and only
  if displayable, so an unaccepted Holder's address matches nothing (ADR 0047) —
  lives inside the API's search predicate, and its load-bearing test is
  `TestSearchingAnUnacceptedHoldersAddressReturnsZeroRows` in the backend's
  integration suite. A copy of the rule on this side would be a second opinion
  about a disclosure and the first one to drift. What is left for these tests is
  the SHAPING: that the term reaches the URL under the API's own name, that it
  composes, and that it counts as a narrowing everywhere a narrowing counts.
*/

test("the search term reaches the URL under the API's name, and leads it", () => {
  assert.equal(holderListQuery(1, filters({ q: "ana@example.com" })), "?q=ana%40example.com");
  // First in the query string, as it is first in the filter bar: a narrowed
  // view reads with the person being looked for at the front.
  assert.equal(
    holderListQuery(1, filters({ q: "Lopez", outstanding: true })),
    "?q=Lopez&outstanding=true",
  );
});

// An empty box is the whole roster and says so by saying nothing — never `q=`,
// which would put a meaningless parameter in every pasted link.
test("a blank search is omitted, not sent empty", () => {
  assert.equal(holderListQuery(1, filters({ q: "" })), "");
});

// The term is carried VERBATIM, encoded and not otherwise touched: the `%` and
// `_` a reader may type are literals the API escapes for LIKE, and a builder
// that stripped or re-cased them here would make the URL disagree with what was
// typed and with what was matched.
test("the search term is carried verbatim, only URL-encoded", () => {
  assert.equal(holderListQuery(1, filters({ q: "50% off_sale" })), "?q=50%25+off_sale");
  assert.equal(holderListQuery(1, filters({ q: "Ana Lopez" })), "?q=Ana+Lopez");
});

test("the search composes with every other filter", () => {
  assert.equal(
    holderListQuery(
      2,
      filters({
        q: "Lopez",
        outstanding: true,
        questionId: "q_size",
        assignmentState: "assigned",
        ticketTypeId: "tt_vip",
        channel: "in_person",
        soldFrom: "2026-07-01",
        soldTo: "2026-07-31",
      }),
    ),
    "?page=2&q=Lopez&outstanding=true&question_id=q_size&assignment_state=assigned" +
      "&ticket_type_id=tt_vip&channel=in_person&sold_from=2026-07-01&sold_to=2026-07-31",
  );
});

// The Clear control's presence, and the empty state's wording: a reader who
// searched and found nobody must be offered the way back, and must be told
// "nothing matched" rather than that the roster is empty.
test("a search alone makes the view narrowed", () => {
  assert.equal(hasActiveHolderListFilters(filters({ q: "Lopez" })), true);
  assert.equal(hasActiveHolderListFilters(EMPTY_HOLDER_LIST_FILTERS), false);
});

// AND IT IS NOT THE CONGRATULATION'S CUE. An empty "outstanding, searched for
// Lopez" view means no Lopez owes anything — not that every required question
// on the Event has been answered, which is what that sentence claims.
test("outstanding is not the sole filter when a search narrows the view too", () => {
  assert.equal(isOutstandingTheOnlyFilter(filters({ outstanding: true, q: "Lopez" })), false);
  assert.equal(isOutstandingTheOnlyFilter(filters({ outstanding: true })), true);
});

// The Clear control returns the whole roster, which since #526 includes
// emptying the box: a Clear that left a search in the URL would hand back a
// list still missing people, with nothing on screen saying why.
test("clearing the filters clears the search too", () => {
  assert.equal(EMPTY_HOLDER_LIST_FILTERS.q, "");
  assert.equal(holderListQuery(1, EMPTY_HOLDER_LIST_FILTERS), "");
});

/*
  THE EMPTY-STATE SELECTION RULE (#528, ADR 0065). Three facts, three sentences,
  and this is the one rule on the screen that can be wrong invisibly: every
  branch renders a plausible grey paragraph and only the fact behind it differs,
  so the branch a reviewer would have to reason about is asserted here instead.
*/

test("an unfiltered empty roster says nothing has been sold", () => {
  assert.equal(holderListEmptyStateKey(EMPTY_HOLDER_LIST_FILTERS), "noTickets");
});

// The congratulation, and ONLY here: it claims every required question on the
// whole Event has been answered, which an empty view supports only when
// `outstanding` is the sole narrowing.
test("an empty outstanding-only view is the congratulation", () => {
  assert.equal(holderListEmptyStateKey(filters({ outstanding: true })), "nothingOutstanding");
});

// Every other narrowing, alone and paired with `outstanding`: "no VIP owes
// anything" is true and is not "every question has been answered".
test("any other empty view says nothing matched", () => {
  for (const narrowing of [
    { q: "Lopez" },
    { questionId: "q_size" },
    { assignmentState: "never_accepted" as const },
    { ticketTypeId: "tt_vip" },
    { channel: "in_person" as const },
    { soldFrom: "2026-07-01" },
    { soldTo: "2026-07-31" },
  ]) {
    assert.equal(holderListEmptyStateKey(filters(narrowing)), "noMatchingTickets");
    assert.equal(
      holderListEmptyStateKey(filters({ ...narrowing, outstanding: true })),
      "noMatchingTickets",
    );
  }
});

// A SORT IS NOT A FILTER. It is not even an argument to the selector, which is
// the point: a reader who sorted the Outstanding-only view by name narrowed
// nothing and still earns the congratulation, and an unfiltered roster sorted
// any way still says nothing has been sold.
test("sorting the view does not change which empty state it says", () => {
  // The sort cannot reach the rule at all: the selector takes the filters and
  // nothing else, so no later edit can quietly start counting it.
  assert.equal(holderListEmptyStateKey.length, 1);
  // And the view a sorted reader is looking at is still the congratulation's:
  // the sort is in the URL beside `outstanding`, narrowing nothing.
  for (const sort of HOLDER_SORT_FIELDS) {
    const sorted = holderListQuery(1, filters({ outstanding: true }), sort, "desc");
    assert.ok(sorted.includes("outstanding=true"));
    assert.equal(holderListEmptyStateKey(filters({ outstanding: true })), "nothingOutstanding");
  }
});

// --- the Holder Export's URL (#529, ADR 0065) ------------------------------

/*
  THE FILE MIRRORS THE SCREEN, AND THIS IS WHERE THAT PROMISE IS KEPT ON THIS
  SIDE OF THE WIRE. `holderExportPath` is built from the SAME two append helpers
  as `holderListQuery` and `fetchHolderList`, so the address bar, the list
  request and the download cannot describe three different rosters. These tests
  are what notices if somebody ever assembles it by hand "just for the export".

  The comparison is against `holderListQuery` itself rather than against
  hand-written strings, because the property is an IDENTITY and not a spelling:
  a filter renamed in the URL must move both, or neither test means anything.
*/

test("the export path carries exactly the filters the URL carries", () => {
  for (const view of [
    filters(),
    filters({ outstanding: true }),
    filters({ q: "ana@example.com" }),
    filters({ questionId: "q-1", assignmentState: "never_accepted" }),
    filters({ ticketTypeId: "tt-1", channel: "online" }),
    filters({ soldFrom: "2026-07-01", soldTo: "2026-07-31" }),
  ]) {
    assert.equal(
      holderExportPath("evt-1", view),
      `/api/events/evt-1/holder-list/export${holderListQuery(1, view)}`,
    );
  }
});

test("the export path carries a non-default sort whole, and omits the default", () => {
  assert.equal(holderExportPath("evt-1", filters()), "/api/events/evt-1/holder-list/export");
  assert.equal(
    holderExportPath("evt-1", filters(), DEFAULT_HOLDER_SORT, DEFAULT_HOLDER_DIR),
    "/api/events/evt-1/holder-list/export",
  );
  assert.equal(
    holderExportPath("evt-1", filters(), "holder", "desc"),
    "/api/events/evt-1/holder-list/export?sort=holder&dir=desc",
  );
});

// PAGINATION IS DELIBERATELY ABSENT: the file is the whole answer, not a page of
// it. A page number reaching the download would hand somebody fifty of their
// four hundred attendees with nothing in the file saying so.
test("the export path never carries a page", () => {
  const path = holderExportPath("evt-1", filters({ outstanding: true }), "buyer", "desc");
  assert.ok(!path.includes("page"), path);
});

// The filters are ENCODED, not interpolated: a search term with a `&` or a space
// in it must not split into two parameters or truncate the search.
test("the export path escapes a search term", () => {
  assert.equal(
    holderExportPath("evt-1", filters({ q: "a b&c=d" })),
    "/api/events/evt-1/holder-list/export?q=a+b%26c%3Dd",
  );
});

// --- the Holder's Dossier (#640) ---------------------------------------------------

// Only an ACCEPTED Holder is a Customer this Event can name; an address a buyer
// typed and nobody accepted offers no link, and neither does a purged one.
test("only an accepted Holder's Dossier can be opened from a row", () => {
  assert.equal(
    holderDossierCustomerId(holderRow({ assignment_state: "accepted", holder_customer_id: "cus_holder" })),
    "cus_holder",
  );
  assert.equal(
    holderDossierCustomerId(holderRow({ assignment_state: "assigned", holder_customer_id: "cus_holder" })),
    null,
  );
  assert.equal(holderDossierCustomerId(holderRow({ assignment_state: "assigned", never_accepted: true })), null);
  assert.equal(holderDossierCustomerId(holderRow({ assignment_state: "unassigned" })), null);
  assert.equal(holderDossierCustomerId(holderRow({})), null);
});

test("an accepted row the API sent without a Holder id offers no link", () => {
  assert.equal(holderDossierCustomerId(holderRow({ assignment_state: "accepted" })), null);
  assert.equal(holderDossierCustomerId(holderRow({ assignment_state: "accepted", holder_customer_id: "" })), null);
});

// THE DOWNLOAD (ADR 0075). The export streams with no size limit, so a failure
// part way arrives as a broken body rather than an envelope. These stub the
// browser's fetch, document and object URLs, and watch what gets saved.
function stubDownloadBrowser(t: { after: (fn: () => void) => void }, response: Response) {
  const saved: string[] = [];
  const original = {
    fetch: globalThis.fetch,
    document: (globalThis as { document?: unknown }).document,
    createObjectURL: URL.createObjectURL,
    revokeObjectURL: URL.revokeObjectURL,
  };
  globalThis.fetch = async () => response;
  URL.createObjectURL = () => "blob:holder-export";
  URL.revokeObjectURL = () => {};
  (globalThis as { document?: unknown }).document = {
    body: { appendChild: () => {} },
    createElement: () => ({
      href: "",
      download: "",
      click(this: { download: string }) {
        saved.push(this.download);
      },
      remove: () => {},
    }),
  };
  t.after(() => {
    globalThis.fetch = original.fetch;
    (globalThis as { document?: unknown }).document = original.document;
    URL.createObjectURL = original.createObjectURL;
    URL.revokeObjectURL = original.revokeObjectURL;
  });
  return saved;
}

test("a finished download is saved under the API's filename", async (t) => {
  const saved = stubDownloadBrowser(
    t,
    new Response(new Uint8Array([0x50, 0x4b]), {
      status: 200,
      headers: { "Content-Disposition": 'attachment; filename="holders-fest-2026-09-22.xlsx"' },
    }),
  );
  await downloadHolderExport("evt_1", EMPTY_HOLDER_LIST_FILTERS);
  assert.deepEqual(saved, ["holders-fest-2026-09-22.xlsx"]);
});

// A stream cut part way is a failed download and saves NO file: a roster missing
// its last people must never reach anybody's Downloads folder.
test("a download cut part way rejects and saves no file", async (t) => {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array([0x50, 0x4b, 0x03, 0x04]));
      controller.error(new TypeError("terminated"));
    },
  });
  const saved = stubDownloadBrowser(t, new Response(body, { status: 200 }));
  await assert.rejects(downloadHolderExport("evt_1", EMPTY_HOLDER_LIST_FILTERS));
  assert.deepEqual(saved, []);
});
