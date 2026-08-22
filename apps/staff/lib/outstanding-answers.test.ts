import assert from "node:assert/strict";
import { test } from "node:test";

import {
  ASSIGNMENT_STATE_KEYS,
  SALES_CHANNEL_KEYS,
  assignmentStateBadgeVariant,
  buyerName,
  holderListVisible,
  holderName,
} from "./outstanding-answers.ts";
import type { TicketOwingAnswers } from "./outstanding-answers.ts";

const ticket = (first: string, last: string): TicketOwingAnswers => ({
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

// THE HOLDER LIST (#329, ADR 0047). Who is coming on a Ticket, drawn beside what
// it owes. Nothing here decides a state — the API derives it from three columns
// so that the buyer's page, this list and the export cannot disagree — and these
// tests hold the shaping, which is all this module does.

const holderRow = (fields: Partial<TicketOwingAnswers>): TicketOwingAnswers => ({
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
// drawn as a blank cell — and so that a purged Ticket, which arrives as
// `unassigned`, is named like any other unassigned one.
test("every assignment state has a key", () => {
  assert.deepEqual(ASSIGNMENT_STATE_KEYS, {
    unassigned: "holderUnassigned",
    assigned: "holderAssigned",
    accepted: "holderAccepted",
  });
});

test("the state decides the badge, in one place", () => {
  assert.equal(assignmentStateBadgeVariant("accepted"), "success");
  assert.equal(assignmentStateBadgeVariant("assigned"), "warning");
  assert.equal(assignmentStateBadgeVariant("unassigned"), "outline");
});

// THE FEATURE FLAG IS THE PAYLOAD'S ABSENCE AND NOTHING ELSE. With
// TICKET_ASSIGNMENT_ENABLED closed the API omits every assignment field, so this
// app holds no second copy of a deployment flag it cannot see (ADR 0045) — and
// the Holder column disappears rather than filling a screen with a word nobody
// can act on.
test("the Holder column is hidden when no row carries an assignment", () => {
  assert.equal(holderListVisible([]), false);
  assert.equal(holderListVisible([ticket("Ana", "López")]), false);
  assert.equal(holderListVisible([holderRow({ assignment_state: "unassigned" })]), true);
});
