import assert from "node:assert/strict";
import { test } from "node:test";

import { SALES_CHANNEL_KEYS, buyerName } from "./outstanding-answers.ts";
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
