/**
 * The buyer's Ticket Assignments, as rules rather than as markup (#324, parent
 * #322).
 *
 * WHAT THIS FEATURE IS FOR, ON THIS SURFACE ONLY. A buyer of four tickets has
 * four Tickets that are indistinguishable from one another: the Answer Links
 * deliberately disclose nothing, so "ticket 2 of 4" was the whole of what could
 * be said to tell them apart. Naming an address for each one is what finally
 * lets the buyer say which is which — the address is the label, and showing it
 * back is half the point of the write.
 *
 * IT LIVES BESIDE lib/buyer-answers.ts AND NOT INSIDE IT, because the two are
 * separate features on one payload and are behind SEPARATE FLAGS. A deployment
 * may have Ticket Questions and no assignment, and the page must look exactly as
 * it did before this shipped in that case — so every rule about "is there
 * anything to draw" is asked of assignment on its own terms.
 *
 * THE FLAG IS READ OFF THE PAYLOAD AND NOWHERE ELSE. With
 * TICKET_ASSIGNMENT_ENABLED closed the API omits every assignment field
 * (`omitempty`, on purpose — see service/buyer_answers.go), so an absent
 * `assignment_state` is how this app learns the feature is dark. There is no
 * second copy of the flag in this app, exactly as there is none for Ticket
 * Questions.
 *
 * Pure and dependency-free — no React, no i18n runtime, no fetch — so it is
 * directly unit-testable, exactly as its two siblings are. It returns SHAPES and
 * TOKENS and never a sentence: the Storefront owns the words, in the reader's
 * language, and the API deliberately sends refusal tokens rather than prose.
 */

import type { BuyerTicket } from "@/lib/buyer-answers";

/**
 * The three states of one Ticket, as the API spells them.
 *
 * `accepted` IS UNREACHABLE TODAY and modelled anyway. #324 sends no mail, so
 * there is no Assignment Link to click; #325 is what makes it happen. Drawing it
 * now means the surface that has to show it later is already written, rather
 * than being found and changed under time pressure.
 */
export type AssignmentState = "unassigned" | "assigned" | "accepted";

/**
 * Why a Ticket may not be assigned right now — the API's own tokens
 * (service/ticket_assignment.go), never re-derived here.
 *
 * A CLOSED WINDOW IS NOT A HIDDEN SECTION. A door sale, a reversed purchase and
 * an Event that has already happened all keep showing who was assigned to what;
 * what closes is the input, not the record. The same posture the Answer window
 * takes one file over.
 */
export type AssignmentRefusal = "channel_unsupported" | "sale_reversed" | "event_started";

/**
 * The body of an assignment write. One field, because the buyer names an ADDRESS
 * and nothing else — a name they typed would be a fact about a person recorded
 * from somebody else's memory, and it would go straight into the Organization's
 * guest list.
 */
export type AssignmentBody = { holder_email: string };

/**
 * The longest a Holder address may be (RFC 5321's forward path), matching
 * catalog.MaxHolderEmailLength.
 *
 * Mirrored rather than left to the API because this is the one text field on
 * this platform typed by one person ABOUT A DIFFERENT PERSON: nobody signs in
 * with it and nobody corrects it but the buyer, so a bound at the moment of
 * typing is the only bound there is.
 */
export const MAX_HOLDER_EMAIL_LENGTH = 254;

/**
 * Whether this deployment offers assignment at all for this Ticket.
 *
 * THE ABSENCE OF THE FIELD IS THE ANSWER. With the flag closed the row carries
 * no `assignment_state`, and a page that drew an "add an address" box anyway
 * would be offering a control whose every press is answered 404. Undefined and
 * not empty-string: `unassigned` is a real state that must be drawn.
 */
export function assignmentOffered(ticket: BuyerTicket): boolean {
  return ticket.assignment_state !== undefined;
}

/**
 * Whether any Ticket on this sale offers assignment — i.e. whether this section
 * has an assignment half at all.
 */
export function saleOffersAssignment(tickets: BuyerTicket[]): boolean {
  return tickets.some(assignmentOffered);
}

/**
 * This Ticket's state, or null while the feature is dark.
 *
 * Read off the payload and never derived from the address and the timestamps
 * here. The platform has ONE definition of the state (catalog.AssignmentState)
 * and a browser recomputing it would be a second opinion that first disagrees on
 * the case nobody thinks about — an accepted Ticket whose Holder address is
 * being changed.
 */
export function assignmentStateOf(ticket: BuyerTicket): AssignmentState | null {
  const state = ticket.assignment_state;
  return state === "unassigned" || state === "assigned" || state === "accepted" ? state : null;
}

/**
 * Whether this Ticket is the buyer's own (ADR 0048): its Holder is the buyer.
 * The page says "your ticket" on it, asks the buyer to answer it, and offers
 * no link for it — there is nobody to forward one to.
 */
export function isOwnTicket(ticket: BuyerTicket): boolean {
  return ticket.self_held === true;
}

/** The address the buyer gave this Ticket, or "" while it has none. */
export function holderEmailOf(ticket: BuyerTicket): string {
  return ticket.holder_email ?? "";
}

/** Whether the buyer may type an address for this Ticket right now. */
export function canAssign(ticket: BuyerTicket): boolean {
  return assignmentOffered(ticket) && ticket.assignable === true;
}

/**
 * Why the input is closed, or null when it is open (or when there is no feature
 * here to be closed).
 *
 * An unrecognised token answers null rather than a guess, for ADR 0023's reason
 * one level over: a token this app has never heard of must degrade to saying
 * nothing, not to saying the wrong thing about somebody's purchase.
 */
export function assignmentRefusalOf(ticket: BuyerTicket): AssignmentRefusal | null {
  if (!assignmentOffered(ticket) || ticket.assignable === true) return null;
  const refusal = ticket.assignable_refusal;
  if (
    refusal === "channel_unsupported" ||
    refusal === "sale_reversed" ||
    refusal === "event_started"
  ) {
    return refusal;
  }
  return null;
}

/**
 * The address as it will be stored: trimmed and lowercased, matching
 * platform.NormalizeEmail, which is the single point at which an address is
 * normalised on this platform.
 *
 * IT MATTERS MORE HERE THAN ANYWHERE. #325 mints or matches a Customer from a
 * click at this address; one stored as the buyer capitalised it would fail to
 * match the record that person already has, quietly turning one person into two.
 * Normalising on this side as well is what makes "is this already the address?"
 * below answer the same as the API's own no-op check.
 */
export function normalizeHolderEmail(raw: string): string {
  return raw.trim().toLowerCase();
}

/**
 * The pre-flight verdict on what the buyer typed: an API error code, or null to
 * send it.
 *
 * IT ANSWERS WITH THE API'S OWN CODE and never a sentence of its own — ADR 0023,
 * and the same arrangement lib/tax-id.ts and lib/phone.ts have. One code, one
 * catalog entry, one sentence: a typo caught here reads exactly as a typo caught
 * by the API reads, in whichever language the page is being read in.
 *
 * IT IS DELIBERATELY WEAKER THAN THE API'S CHECK and must stay that way. An
 * address is only really validated by mail arriving at it, and a mirror that
 * refused more than the API does would block a buyer from saving an address the
 * platform would have accepted — a form refusing a correct answer, with no
 * appeal. So it catches only what is certainly not an address: nothing at all,
 * something longer than an address may be, anything with whitespace in it
 * (which is the display-name form `Ana <ana@example.com>` that mail.ParseAddress
 * accepts and catalog.ParseHolderEmail then refuses), and the missing `@`.
 *
 * THE MISSING `@` IS THE ONE WORTH CATCHING. Until #325 mails the address,
 * nobody discovers it is wrong: the buyer sees their own typo echoed back and
 * believes the job done.
 */
export function holderEmailRefusal(raw: string): "INVALID_HOLDER_EMAIL" | null {
  const email = normalizeHolderEmail(raw);
  if (email === "" || email.length > MAX_HOLDER_EMAIL_LENGTH) return "INVALID_HOLDER_EMAIL";
  if (/\s/.test(email)) return "INVALID_HOLDER_EMAIL";
  // Exactly one `@`, with something on each side of it. Nothing is said about
  // what that something may contain — the API decides, and this must not become
  // a second opinion about apostrophes, plus addressing or new top-level
  // domains.
  const parts = email.split("@");
  if (parts.length !== 2 || parts[0] === "" || parts[1] === "") return "INVALID_HOLDER_EMAIL";
  return null;
}

/**
 * Whether sending this would change anything.
 *
 * The API treats re-sending the address a Ticket already carries as a no-op that
 * moves no timestamp, so this is not a correctness guard — it is here so the
 * form can say "that is already this ticket's address" instead of reporting a
 * save that saved nothing. On a REASSIGNMENT the distinction has teeth: a
 * genuine change clears that Ticket's Answers, and a buyer who pressed save on
 * an unchanged field must not be told their answers went with it.
 */
export function isUnchangedAssignment(ticket: BuyerTicket, raw: string): boolean {
  const current = holderEmailOf(ticket);
  return current !== "" && normalizeHolderEmail(raw) === current;
}

/**
 * The body to send, or null when there is nothing worth sending.
 *
 * The address goes NORMALISED rather than as typed. The API normalises it again
 * — it must, since nothing about a browser is trusted — and the two agreeing is
 * what keeps the value echoed back on the next render identical to the value the
 * buyer is looking at in the field.
 */
export function assignmentBodyFor(raw: string): AssignmentBody | null {
  if (holderEmailRefusal(raw) !== null) return null;
  return { holder_email: normalizeHolderEmail(raw) };
}

/**
 * How many of this sale's Tickets carry an address, out of how many could.
 *
 * PARTIAL ASSIGNMENT IS THE NORMAL CASE and not a half-finished one: a sale of
 * four where the buyer knows two addresses is complete as far as they are
 * concerned, and nothing on this page may read as an error because of it. So the
 * summary counts rather than warns, and the denominator is the Tickets that CAN
 * be assigned — counting a door sale's Tickets in a total the buyer can never
 * move would be a page setting somebody a goal it will not let them reach.
 */
export function assignmentTally(tickets: BuyerTicket[]): { assigned: number; total: number } {
  const offered = tickets.filter(assignmentOffered);
  return {
    assigned: offered.filter((ticket) => holderEmailOf(ticket) !== "").length,
    total: offered.length,
  };
}
