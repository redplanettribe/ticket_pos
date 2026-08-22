import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { apiErrorMessage, type ErrorCatalog } from "./api-errors.ts";
import type { BuyerTicket } from "./buyer-answers.ts";
import {
  assignmentBodyFor,
  assignmentOffered,
  assignmentRefusalOf,
  assignmentStateOf,
  assignmentTally,
  canAssign,
  holderEmailOf,
  holderEmailRefusal,
  isUnchangedAssignment,
  MAX_HOLDER_EMAIL_LENGTH,
  normalizeHolderEmail,
  saleOffersAssignment,
} from "./ticket-assignment.ts";

/**
 * The buyer's Ticket Assignments, as rules (#324, parent #322).
 *
 * These assert the decisions that change what a person SEES or SENDS, and
 * deliberately do not re-test what a valid Holder address is: that is decided on
 * the server, once, by catalog.ParseHolderEmail. What is tested here is that
 * this side is WEAKER than that check rather than stricter, that the flag is
 * read off the payload's absence rather than guessed, and that partial
 * assignment is counted rather than complained about.
 */

function ticket(overrides: Partial<BuyerTicket> = {}): BuyerTicket {
  return {
    ticket_id: "t1",
    ordinal: 1,
    ticket_type_name: "General",
    answer_link: "https://example.test/answer?token=x",
    answerable: true,
    answerable_refusal: "",
    outstanding_count: 0,
    questions: [],
    // The shape a deployment with TICKET_ASSIGNMENT_ENABLED open sends.
    assignment_state: "unassigned",
    assignable: true,
    assignable_refusal: "",
    ...overrides,
  };
}

/** A row exactly as a deployment with the flag CLOSED sends it: every
 * assignment field omitted, not emptied. */
function darkTicket(overrides: Partial<BuyerTicket> = {}): BuyerTicket {
  const row = ticket(overrides);
  delete row.assignment_state;
  delete row.holder_email;
  delete row.assignable;
  delete row.assignable_refusal;
  return row;
}

// THE FLAG IS THE ABSENCE OF THE FIELD, and nothing else in this app knows about
// it. A page that drew an address box for a dark deployment would be offering a
// control whose every press is answered 404.
test("a payload with no assignment fields is a deployment that has no assignment", () => {
  assert.equal(assignmentOffered(darkTicket()), false);
  assert.equal(saleOffersAssignment([darkTicket(), darkTicket({ ticket_id: "t2" })]), false);
  assert.equal(canAssign(darkTicket()), false);
  assert.equal(assignmentStateOf(darkTicket()), null);
  assert.equal(assignmentRefusalOf(darkTicket()), null);
});

// `unassigned` is a REAL STATE and must be drawn — the difference between "no
// address yet" and "this deployment has no such feature".
test("an unassigned ticket is offered assignment, and says so", () => {
  assert.equal(assignmentOffered(ticket()), true);
  assert.equal(assignmentStateOf(ticket()), "unassigned");
  assert.equal(holderEmailOf(ticket()), "");
  assert.equal(canAssign(ticket()), true);
});

test("one assignable ticket is enough for the sale to draw the section", () => {
  assert.equal(saleOffersAssignment([darkTicket(), ticket({ ticket_id: "t2" })]), true);
  assert.equal(saleOffersAssignment([]), false);
});

// The state is READ and never derived here. The platform has one definition
// (catalog.AssignmentState) and a browser recomputing it would first disagree on
// the case nobody thinks about.
test("the state is taken from the server, and an unknown one is not guessed at", () => {
  assert.equal(
    assignmentStateOf(ticket({ assignment_state: "assigned", holder_email: "ana@example.test" })),
    "assigned",
  );
  assert.equal(
    assignmentStateOf(ticket({ assignment_state: "accepted", holder_email: "ana@example.test" })),
    "accepted",
  );
  assert.equal(assignmentStateOf(ticket({ assignment_state: "pending" })), null);
});

// A CLOSED WINDOW HIDES THE INPUT AND NEVER THE RECORD. A door sale, a reversed
// purchase and an Event that has happened all keep showing who was assigned to
// what.
test("a closed window names its reason and keeps the address readable", () => {
  const closed = ticket({
    assignment_state: "assigned",
    holder_email: "ana@example.test",
    assignable: false,
    assignable_refusal: "sale_reversed",
  });
  assert.equal(canAssign(closed), false);
  assert.equal(assignmentRefusalOf(closed), "sale_reversed");
  assert.equal(holderEmailOf(closed), "ana@example.test");
  assert.equal(assignmentStateOf(closed), "assigned");
});

test("each of the API's refusal tokens is understood, and an unknown one says nothing", () => {
  for (const token of ["channel_unsupported", "sale_reversed", "event_started"]) {
    const closed = ticket({ assignable: false, assignable_refusal: token });
    assert.equal(assignmentRefusalOf(closed), token);
  }
  // ADR 0023's posture, one level over: a token this app has never heard of
  // degrades to saying nothing rather than to saying the wrong thing.
  assert.equal(
    assignmentRefusalOf(ticket({ assignable: false, assignable_refusal: "purged" })),
    null,
  );
  // An OPEN window has no reason to give.
  assert.equal(assignmentRefusalOf(ticket()), null);
});

// NORMALISED THE WAY THE PLATFORM NORMALISES. #325 mints or matches a Customer
// from a click at this address, and one stored as the buyer capitalised it would
// fail to match the record that person already has — quietly turning one person
// into two.
test("the address is trimmed and lowercased, exactly as platform.NormalizeEmail does", () => {
  assert.equal(normalizeHolderEmail("  Ana@Example.Test \n"), "ana@example.test");
  assert.deepEqual(assignmentBodyFor("  Ana@Example.Test "), {
    holder_email: "ana@example.test",
  });
});

// THE MIRROR CATCHES ONLY WHAT IS CERTAINLY NOT AN ADDRESS. A mirror stricter
// than the API would block a buyer from saving something the platform would have
// accepted — a form refusing a correct answer, with no appeal.
test("the pre-flight refuses nothing, a missing @, whitespace and an over-long address", () => {
  assert.equal(holderEmailRefusal(""), "INVALID_HOLDER_EMAIL");
  assert.equal(holderEmailRefusal("   "), "INVALID_HOLDER_EMAIL");
  assert.equal(holderEmailRefusal("ana.example.test"), "INVALID_HOLDER_EMAIL");
  assert.equal(holderEmailRefusal("ana@@example.test"), "INVALID_HOLDER_EMAIL");
  assert.equal(holderEmailRefusal("@example.test"), "INVALID_HOLDER_EMAIL");
  assert.equal(holderEmailRefusal("ana@"), "INVALID_HOLDER_EMAIL");
  // The display-name form, which mail.ParseAddress accepts and
  // catalog.ParseHolderEmail then refuses: what is stored must be an address and
  // nothing else, because it travels into a mail header and into the
  // Organization's export.
  assert.equal(holderEmailRefusal("Ana <ana@example.test>"), "INVALID_HOLDER_EMAIL");
  const tooLong = `${"a".repeat(MAX_HOLDER_EMAIL_LENGTH)}@example.test`;
  assert.equal(holderEmailRefusal(tooLong), "INVALID_HOLDER_EMAIL");
  assert.equal(assignmentBodyFor("ana.example.test"), null);
});

// The addresses people actually have. This side must not be a second opinion
// about plus addressing, apostrophes or new top-level domains — the API decides.
test("the pre-flight lets through the addresses the API is the judge of", () => {
  for (const address of [
    "ana@example.test",
    "ana+festival@example.test",
    "o'neill@example.test",
    "ana.maria@sub.domain.example",
    "ana@localhost",
    "ANA@EXAMPLE.TEST",
  ]) {
    assert.equal(holderEmailRefusal(address), null, address);
  }
});

// ONE CODE, ONE SENTENCE (ADR 0023). The mirror answers with the API's own code,
// so a typo caught before the request and a typo caught by
// catalog.ParseHolderEmail read identically — and in either language.
test("the code the mirror answers with has copy in every catalog", () => {
  const code = holderEmailRefusal("nope");
  assert.equal(code, "INVALID_HOLDER_EMAIL");
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { errors: ErrorCatalog };
    const sentence = apiErrorMessage(catalog.errors, { code });
    assert.ok(sentence && sentence.trim() !== "", `${locale} has no copy for ${code}`);
  }
});

// Re-sending the stored address is a no-op on the API. It is caught here so the
// form can say so rather than report a save that saved nothing — which matters
// on a REASSIGNMENT, where a real save clears this Ticket's Answers and this one
// did not.
test("re-sending the address a ticket already carries is recognised as no change", () => {
  const assigned = ticket({ assignment_state: "assigned", holder_email: "ana@example.test" });
  assert.equal(isUnchangedAssignment(assigned, "Ana@Example.Test"), true);
  assert.equal(isUnchangedAssignment(assigned, " ana@example.test "), true);
  assert.equal(isUnchangedAssignment(assigned, "bea@example.test"), false);
  // A FIRST assignment is never "unchanged": there was nothing there to keep.
  assert.equal(isUnchangedAssignment(ticket(), "ana@example.test"), false);
});

// PARTIAL ASSIGNMENT IS THE NORMAL CASE. A sale of four where the buyer knows
// two addresses is finished as far as they are concerned, and nothing on the
// page may read as an error because of it.
test("a sale of four with two addresses counts two of four and refuses nothing", () => {
  const tickets = [
    ticket({ ticket_id: "a", assignment_state: "assigned", holder_email: "ana@example.test" }),
    ticket({ ticket_id: "b", assignment_state: "assigned", holder_email: "bea@example.test" }),
    ticket({ ticket_id: "c" }),
    ticket({ ticket_id: "d" }),
  ];
  assert.deepEqual(assignmentTally(tickets), { assigned: 2, total: 4 });
  // Every one of them is still assignable — the two given are not a gate on the
  // two that are not.
  assert.equal(
    tickets.every((each) => canAssign(each)),
    true,
  );
});

// The denominator is the Tickets that CAN carry an address. Counting a dark
// Ticket in a total the buyer can never move would be a page setting somebody a
// goal it will not let them reach.
test("the tally counts only the tickets the feature reaches", () => {
  assert.deepEqual(
    assignmentTally([
      darkTicket({ ticket_id: "a" }),
      ticket({ ticket_id: "b", assignment_state: "assigned", holder_email: "ana@example.test" }),
    ]),
    { assigned: 1, total: 1 },
  );
  assert.deepEqual(assignmentTally([]), { assigned: 0, total: 0 });
});
