import assert from "node:assert/strict";
import { test } from "node:test";

import {
  ASSIGNMENT_STATE_KEYS,
  SALES_CHANNEL_KEYS,
  assignmentStateBadgeVariant,
  buyerName,
  holderBadgeVariant,
  holderListVisible,
  holderName,
  holderStateKey,
  questionsVisible,
} from "./holder-list.ts";
import type { HolderListPage, HolderTicket } from "./holder-list.ts";

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
