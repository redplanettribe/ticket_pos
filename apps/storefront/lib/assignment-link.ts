/**
 * The Assignment Link's page, as rules rather than as markup (#325, parent
 * #322, ADR 0046).
 *
 * An Assignment Link is the fourth signed link this platform mails, and the only
 * one that MINTS AN IDENTITY: a friend bought a ticket and named an email
 * address, the platform wrote to it, and pressing the link is Proof of Email
 * Ownership (ADR 0035) — so the press makes the reader a Verified Customer, and
 * only then are they asked their name and the Ticket Questions.
 *
 * NOTHING HERE INVENTS A FIELD. What arrives is the Event, the Ticket Type, the
 * reader's OWN name and their questions, and nothing about the purchase: never
 * the buyer, the price, the Tax ID, the Sale Confirmation reference or the
 * Sale's other Tickets. That is a backend property (service.AssignmentLinkView,
 * with an integration test over the raw response body); this file is one reason
 * it stays one.
 *
 * IT REUSES lib/ticket-questions's QUESTION TYPES ON PURPOSE. A Holder answers the
 * same Ticket Questions through the same shapes with the same retired-question
 * rule; a second copy of those types here would be a second place for the rule
 * about retired questions to drift, and a reply visible on one page and gone
 * from the other.
 *
 * Pure and dependency-free — no React, no i18n runtime — so the rules are
 * directly unit-testable. It returns SHAPES and never a sentence.
 */

import type { QuestionAnswer } from "@/lib/ticket-questions";

/**
 * What accepting an Assignment Link returns: FOUR FIELDS AND NO MORE.
 *
 * Written narrow ON PURPOSE. There is no buyer
 * here, no price, no Tax ID, no confirmation reference, no sibling Ticket and
 * not even a ticket id — every write posts back with the TOKEN. Being a Customer
 * of this platform buys nobody a fact about somebody else's purchase.
 *
 * THE NAME IS HERE AND ONLY HERE. It arrives on the far side of the press, never
 * before: a page reachable without the press that showed a known Customer's name
 * would be an oracle for whether an address is registered (ADR 0035). There is
 * no route in this app that asks for it any earlier, and no page state that
 * holds it before the press returns.
 *
 * AND THERE IS NO TAX ID FIELD, which is a deliberate absence rather than an
 * omission: a Tax ID is a fact about the sale's BUYER and is never asked of an
 * attendee (ADR 0046).
 */
export type AssignmentLinkView = {
  event_name: string;
  ticket_type_name: string;
  /** The reader's own current name, empty for somebody never named before. */
  holder_first_name: string;
  holder_last_name: string;
  questions: QuestionAnswer[];
};

/**
 * The `errors.assignmentLink` catalog key for each way the press can fail.
 *
 * Mapped here rather than left to the API's own sentence because the RECOVERY
 * differs, and because this reader has the least recourse of anybody on the
 * platform: they do not know who bought the ticket, so "ask whoever sent it" —
 * which the Answer Link's page can honestly say — is advice they cannot follow.
 *
 * The invalid case deliberately covers a reassigned Ticket and a reversed Sale
 * as well as a forgery. The API refuses all three identically, because "your
 * friend gave your ticket to someone else" and "your friend cancelled the
 * purchase" are facts about the buyer's decisions, and this page never names the
 * buyer or describes what they did — CONTEXT.md says the disclosure rule holds
 * in the error state too.
 */
export const ASSIGNMENT_LINK_FAILURE_KEYS = {
  ASSIGNMENT_LINK_INVALID: "invalid",
  ASSIGNMENT_LINK_EXPIRED: "expired",
  // The feature flag is closed. It answers 404 exactly as a build without the
  // feature does (ADR 0045), and the reader is told the link does not work —
  // the true statement available to them.
  TICKET_ASSIGNMENT_UNAVAILABLE: "invalid",
  // No signing key configured: a deployment fault, not the reader's, so it says
  // "try again later" rather than blaming a link they cannot replace.
  ASSIGNMENT_LINK_UNAVAILABLE: "unavailable",
} as const;

export type AssignmentLinkFailure =
  (typeof ASSIGNMENT_LINK_FAILURE_KEYS)[keyof typeof ASSIGNMENT_LINK_FAILURE_KEYS];

/**
 * Which copy a failed press gets, from the API's error code.
 *
 * A code this app has not heard of falls through to "invalid", which is the
 * honest floor: the reader could not accept their ticket, and the page has
 * nothing truer to tell them than that.
 */
export function assignmentLinkFailure(code: string | undefined): AssignmentLinkFailure {
  if (code && code in ASSIGNMENT_LINK_FAILURE_KEYS) {
    return ASSIGNMENT_LINK_FAILURE_KEYS[code as keyof typeof ASSIGNMENT_LINK_FAILURE_KEYS];
  }
  return "invalid";
}

/** The body the name PUT takes: the two halves, plus the token. */
export type HolderNameBody = {
  first_name: string;
  last_name: string;
};

/**
 * Whether the name as typed is worth sending.
 *
 * BOTH HALVES, NON-BLANK, mirroring catalog.ParseHolderName — which is the one
 * that decides, and which refuses this in the API whatever this function
 * returns. What this buys is not validation but manners: a reader who left a
 * field empty is told so before a round trip, and the button says why it is off.
 *
 * It does NOT trim into the payload. The API normalises, and a client that
 * trimmed would be a second definition of what a name is.
 */
export function holderNameIsGiven(firstName: string, lastName: string): boolean {
  return firstName.trim() !== "" && lastName.trim() !== "";
}
