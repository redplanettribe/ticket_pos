import assert from "node:assert/strict";
import { test } from "node:test";

import {
  ASSIGNMENT_STATE_KEYS,
  HOLDER_LIST_CHANNELS,
  DEFAULT_HOLDER_DIR,
  DEFAULT_HOLDER_SORT,
  EMPTY_HOLDER_LIST_FILTERS,
  SALES_CHANNEL_KEYS,
  assignmentStateBadgeVariant,
  buyerName,
  hasActiveHolderListFilters,
  holderBadgeVariant,
  holderListQuery,
  holderListVisible,
  holderName,
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
// with no `sort` would be read against whatever the default field becomes when
// #527 adds the other four.
test("a non-default sort is carried whole", () => {
  assert.equal(holderListQuery(1, filters(), "sold_at", "desc"), "?sort=sold_at&dir=desc");
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
  assert.equal(parseHolderSort("amount"), DEFAULT_HOLDER_SORT);
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
