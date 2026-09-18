import assert from "node:assert/strict";
import test from "node:test";

import {
  dossierBackHref,
  dossierHref,
  nameGivenOnSale,
  saleStatusBadgeVariant,
  saleStatusToken,
} from "./customer-dossier.ts";
import {
  assignmentRemindersVisible,
  dossierTicketBadgeVariant,
  dossierTicketStateKey,
  heldTicketBuyerName,
  heldTicketOnReversedSale,
  heldTicketsVisible,
  holderNameOnTicket,
  nameGivenAsHolder,
  ticketHolderDossierId,
} from "./customer-dossier.ts";
import type { DossierHeldTicket, DossierTicket } from "./customer-dossier.ts";

const EVENT = "abc";
const LIST = "/events/abc/sales";

// --- dossierHref -------------------------------------------------------------

test("dossierHref addresses the Dossier by customer id and carries the list back", () => {
  assert.equal(
    dossierHref(EVENT, "cus_1", "/events/abc/sales?q=ana&page=2"),
    "/events/abc/sales/customers/cus_1?from=%2Fevents%2Fabc%2Fsales%3Fq%3Dana%26page%3D2",
  );
});

test("dossierHref encodes the ids", () => {
  assert.equal(
    dossierHref("a/b", "c?d", LIST),
    "/events/a%2Fb/sales/customers/c%3Fd?from=%2Fevents%2Fabc%2Fsales",
  );
});

test("a from built by dossierHref comes back through dossierBackHref intact", () => {
  const from = "/events/abc/sales?status=reversed&sort=amount&dir=asc&page=3";
  const href = dossierHref(EVENT, "cus_1", from);
  const decoded = new URL(href, "https://staff.example").searchParams.get("from");
  assert.equal(dossierBackHref(decoded, EVENT), from);
});

// --- dossierBackHref: accepted -------------------------------------------------

test("dossierBackHref keeps a path within this Event, query and all", () => {
  for (const from of [
    "/events/abc",
    "/events/abc/",
    "/events/abc/sales",
    "/events/abc/sales?q=ana%40example.com&page=2",
    "/events/abc?tab=x",
    "/events/abc#top",
    "/events/abc/sales/holders?outstanding=true&sort=owes&dir=desc",
    "/events/abc/sales?q=a..b",
  ]) {
    assert.equal(dossierBackHref(from, EVENT), from, from);
  }
});

// --- dossierBackHref: refused ---------------------------------------------------

test("dossierBackHref falls back to the Sales list when from is missing", () => {
  assert.equal(dossierBackHref(undefined, EVENT), LIST);
  assert.equal(dossierBackHref(null, EVENT), LIST);
  assert.equal(dossierBackHref("", EVENT), LIST);
});

test("dossierBackHref refuses absolute URLs, schemes and protocol-relative paths", () => {
  for (const from of [
    "https://evil.example/events/abc/sales",
    "http://evil.example",
    "javascript:alert(1)",
    "//evil.example/events/abc",
    "//events/abc",
    "events/abc/sales",
    " /events/abc/sales",
  ]) {
    assert.equal(dossierBackHref(from, EVENT), LIST, from);
  }
});

test("dossierBackHref refuses another Event, including one whose id starts with this one's", () => {
  for (const from of ["/events/abcd/sales", "/events/ab/sales", "/events/xyz/sales", "/events/abc-2", "/events", "/"]) {
    assert.equal(dossierBackHref(from, EVENT), LIST, from);
  }
});

test("dossierBackHref refuses backslashes", () => {
  for (const from of ["/events/abc\\..\\..\\evil", "/\\evil.example", "/events/abc/sales\\"]) {
    assert.equal(dossierBackHref(from, EVENT), LIST, from);
  }
});

test("dossierBackHref refuses dot segments, literal or encoded", () => {
  for (const from of [
    "/events/abc/..",
    "/events/abc/../xyz/sales",
    "/events/abc/./sales",
    "/events/abc/sales/..?q=1",
    "/events/abc/%2e%2e/xyz",
    "/events/abc/%2E./xyz",
    "/events/abc/.%2e#x",
  ]) {
    assert.equal(dossierBackHref(from, EVENT), LIST, from);
  }
});

test("dossierBackHref refuses control characters", () => {
  for (const from of ["/events/abc/sales\n", "/events/abc\t/sales", "/events/abc/\u0000", "/events/abc/\u007f"]) {
    assert.equal(dossierBackHref(from, EVENT), LIST, JSON.stringify(from));
  }
});

// --- status ----------------------------------------------------------------------

test("saleStatusToken names the four statuses", () => {
  assert.equal(saleStatusToken("active"), "active");
  assert.equal(saleStatusToken("reversed"), "reversed");
  assert.equal(saleStatusToken("corrected"), "corrected");
  assert.equal(saleStatusToken("upgraded"), "upgraded");
});

test("saleStatusToken answers null for a status this app has never heard of", () => {
  assert.equal(saleStatusToken("refunded"), null);
  assert.equal(saleStatusToken("Active"), null);
  assert.equal(saleStatusToken(""), null);
  assert.equal(saleStatusToken(null), null);
  assert.equal(saleStatusToken(undefined), null);
});

test("a Sale that no longer stands is drawn as such, and an unknown one never as live", () => {
  assert.equal(saleStatusBadgeVariant("active"), "secondary");
  assert.equal(saleStatusBadgeVariant("reversed"), "destructive");
  assert.equal(saleStatusBadgeVariant("corrected"), "destructive");
  assert.equal(saleStatusBadgeVariant(null), "outline");
});

// An Upgrade is neither live nor an error, and the badge has to say both things
// at once: not the active chip, because the Sale no longer stands, and not the
// destructive one, because nobody erred (#651, ADR 0074).
test("an upgraded Sale is drawn quietly rather than as an error", () => {
  assert.equal(saleStatusBadgeVariant("upgraded"), "outline");
  assert.notEqual(saleStatusBadgeVariant("upgraded"), saleStatusBadgeVariant("corrected"));
  assert.notEqual(saleStatusBadgeVariant("upgraded"), saleStatusBadgeVariant("active"));
});

// --- name ------------------------------------------------------------------------

test("nameGivenOnSale joins the halves without a stray space", () => {
  assert.equal(nameGivenOnSale({ customer_first_name: "Ana", customer_last_name: "López" }), "Ana López");
  assert.equal(nameGivenOnSale({ customer_first_name: "Ana", customer_last_name: " " }), "Ana");
  assert.equal(nameGivenOnSale({ customer_first_name: "", customer_last_name: "López" }), "López");
  assert.equal(nameGivenOnSale({ customer_first_name: "  ", customer_last_name: "" }), "");
});

// --- Tickets and Holders (#640) ------------------------------------------------------

const dossierTicket = (fields: Partial<DossierTicket> = {}): DossierTicket => ({
  ticket_id: "tk_1",
  ticket_type_id: "tt_1",
  ticket_type_name: "General",
  ordinal: 1,
  ...fields,
});

test("a Ticket's state is named with the Holder List's own words", () => {
  assert.equal(dossierTicketStateKey(dossierTicket({ assignment_state: "unassigned" })), "holderUnassigned");
  assert.equal(dossierTicketStateKey(dossierTicket({ assignment_state: "assigned" })), "holderAssigned");
  assert.equal(dossierTicketStateKey(dossierTicket({ assignment_state: "accepted" })), "holderAccepted");
  assert.equal(
    dossierTicketStateKey(dossierTicket({ assignment_state: "assigned", never_accepted: true })),
    "holderNeverAccepted",
  );
});

// The buyer's Self-held Ticket (ADR 0048) is theirs, not given away.
test("a Ticket the buyer holds themselves reads as their own", () => {
  const own = dossierTicket({ assignment_state: "accepted", self_held: true });
  assert.equal(dossierTicketStateKey(own), "selfHeld");
  assert.equal(dossierTicketBadgeVariant(own), "secondary");
});

// Absent with the flag closed, and always absent on a void Sale's Tickets.
test("a Ticket with no assignment state has no state to draw", () => {
  assert.equal(dossierTicketStateKey(dossierTicket()), null);
  assert.equal(dossierTicketBadgeVariant(dossierTicket()), null);
});

test("a Ticket's badge follows the Holder List's colours", () => {
  assert.equal(dossierTicketBadgeVariant(dossierTicket({ assignment_state: "accepted" })), "success");
  assert.equal(dossierTicketBadgeVariant(dossierTicket({ assignment_state: "assigned" })), "warning");
  assert.equal(dossierTicketBadgeVariant(dossierTicket({ assignment_state: "unassigned" })), "outline");
  assert.equal(
    dossierTicketBadgeVariant(dossierTicket({ assignment_state: "assigned", never_accepted: true })),
    "outline",
  );
});

test("holderNameOnTicket joins the halves and is empty without a Holder", () => {
  assert.equal(
    holderNameOnTicket(dossierTicket({ holder_first_name: "Carla", holder_last_name: "Ruiz" })),
    "Carla Ruiz",
  );
  assert.equal(holderNameOnTicket(dossierTicket({ holder_first_name: " ", holder_last_name: "Ruiz" })), "Ruiz");
  assert.equal(holderNameOnTicket(dossierTicket()), "");
});

test("only an accepted Holder who is somebody else links to their Dossier", () => {
  assert.equal(
    ticketHolderDossierId(dossierTicket({ assignment_state: "accepted", holder_customer_id: "cus_2" })),
    "cus_2",
  );
  assert.equal(
    ticketHolderDossierId(dossierTicket({ assignment_state: "accepted", holder_customer_id: "cus_1", self_held: true })),
    null,
  );
  assert.equal(ticketHolderDossierId(dossierTicket({ assignment_state: "accepted" })), null);
  assert.equal(
    ticketHolderDossierId(dossierTicket({ assignment_state: "assigned", holder_customer_id: "cus_2" })),
    null,
  );
  assert.equal(ticketHolderDossierId(dossierTicket({ holder_customer_id: "cus_2" })), null);
});

// The flag is read off the payload's absence (ADR 0045); an empty list is an
// open flag and a Sale nobody was reminded about.
test("the reminder times are drawn only when the payload carries them", () => {
  assert.equal(assignmentRemindersVisible({}), false);
  assert.equal(assignmentRemindersVisible({ assignment_reminder_sent_at: [] }), true);
  assert.equal(assignmentRemindersVisible({ assignment_reminder_sent_at: ["2026-08-22T10:00:00Z"] }), true);
});

test("the held Tickets section exists only when the payload carries it", () => {
  assert.equal(heldTicketsVisible({}), false);
  assert.equal(heldTicketsVisible({ held_tickets: [] }), true);
});

const heldTicket = (fields: Partial<DossierHeldTicket> = {}): DossierHeldTicket => ({
  ticket_id: "tk_9",
  ticket_type_id: "tt_1",
  ticket_type_name: "General",
  ordinal: 2,
  ticket_sale_id: "s_9",
  confirmation_ref: "XYZ-9",
  sale_status: "active",
  buyer_first_name: "Ana",
  buyer_last_name: "López",
  accepted_at: "2026-08-20T10:00:00Z",
  ...fields,
});

test("a held Ticket names its buyer and the name given as Holder", () => {
  assert.equal(heldTicketBuyerName(heldTicket()), "Ana López");
  assert.equal(heldTicketBuyerName(heldTicket({ buyer_first_name: "", buyer_last_name: " " })), "");
  assert.equal(nameGivenAsHolder(heldTicket({ holder_first_name: "Carla", holder_last_name: "Ruiz" })), "Carla Ruiz");
  assert.equal(nameGivenAsHolder(heldTicket()), "");
});

// Only `active` is a live Ticket; anything else, including a status this app
// has never heard of, is never drawn as one.
test("a held Ticket on a Sale that no longer stands is told apart from a live one", () => {
  assert.equal(heldTicketOnReversedSale(heldTicket()), false);
  assert.equal(heldTicketOnReversedSale(heldTicket({ sale_status: "reversed" })), true);
  assert.equal(heldTicketOnReversedSale(heldTicket({ sale_status: "corrected" })), true);
});
