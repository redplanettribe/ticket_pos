/**
 * The Re-addressing Link's page, as rules rather than as markup (#422, parent
 * #419, ADR 0058).
 *
 * A Re-addressing Link is the Assignment Link's twin: a Platform Operator
 * recorded the address a stranded buyer meant, the platform wrote to it, and
 * pressing the link is Proof of Email Ownership (ADR 0035) — so the press mints
 * or matches a Verified Customer, moves the Sale to them, and lands them signed
 * in on it. The verb is ACCEPT and the act is a re-address; "claim" and
 * "transfer" are barred words (CONTEXT.md) and appear nowhere in this app.
 *
 * Where it differs from the Assignment Link is what may be shown BEFORE the
 * press. The Assignment page shows nothing, because showing a known Customer's
 * name would be an oracle for whether an address is registered. This page shows
 * the Event, the Sale Confirmation reference and the corrected address — the
 * three facts the mail already carried, disclosed by the API's own view
 * endpoint for exactly this purpose — and nothing else: never the buyer's name,
 * the money, the Tax ID or the Tickets. Reading the view WRITES NOTHING, which
 * is what lets a mail scanner fetch the page and change nothing.
 *
 * Pure and dependency-free — no React, no i18n runtime — so the rules are
 * directly unit-testable. It returns SHAPES and never a sentence.
 */

import { customerAreaSaleHref } from "./destination.ts";
import { signInConsentPath } from "./google-signin.ts";

/**
 * What GET /public/re-addressing-link shows before the press: the three facts
 * the mail carried, and whether the link has already been accepted.
 *
 * `accepted_at` is the one fact the mail could not carry. When it is set the
 * page says the purchase is already theirs and STILL offers the button: a mail
 * opened twice, or a button pressed twice, must land the reader on the Sale
 * rather than on an error, and the accept is idempotent on the API's side so
 * that pressing it again rewrites nothing.
 */
export type ReAddressingLinkView = {
  event_name: string;
  confirmation_ref: string;
  corrected_email: string;
  accepted_at: string | null;
};

/**
 * What the BFF answers after a press: the Sale the reader now owns, and whether
 * the Privacy Policy still has to be accepted before a session exists.
 *
 * The session itself never reaches the browser. The BFF holds it in the httpOnly
 * cookie exactly as the passcode verify route does, and when consent is
 * outstanding it parks the pending-consent token in the cookie the Google
 * callback uses, so the sign-in page's own consent step finishes the job.
 */
export type ReAddressingAccepted = {
  ticket_sale_id: string;
  consent_required: boolean;
};

/**
 * The token out of the address, or "" when the address does not carry one.
 *
 * A REPEATED parameter is not a link this platform ever wrote, and taking the
 * first would be guessing at which one somebody meant; so is a token that is
 * only whitespace. The API trims too, so trimming here is not a second
 * definition of what a token is — it is what lets the page tell "no token" from
 * "a token the API refused" before making a request.
 */
export function reAddressingLinkToken(raw: string | string[] | undefined): string {
  return typeof raw === "string" ? raw.trim() : "";
}

/**
 * The `reAddressingLink` catalog key for each way the link can be refused.
 *
 * Unlike the Assignment Link, this page TELLS THE REASONS APART. The
 * Assignment page's reader does not know who bought the ticket, so "your friend
 * gave it to someone else" is a fact about a stranger's decision it must not
 * disclose. This page's reader is the BUYER: it was their purchase, their money
 * and their mistyped address, and the API carries the reason in
 * `details.reason` for that reason alone. A withdrawn re-addressing, a reversed
 * Sale and a started Event are each said plainly.
 */
export const RE_ADDRESSING_LINK_FAILURE_KEYS = {
  // Tampered, truncated, signed for another purpose, or naming a recording
  // that a later one replaced. Deliberately indistinguishable on the API's
  // side; one sentence here.
  RE_ADDRESSING_LINK_INVALID: "invalid",
  // No signing key configured: a deployment fault, not the reader's, so it
  // says "try again later" rather than blaming a link they cannot replace.
  RE_ADDRESSING_LINK_UNAVAILABLE: "unavailable",
} as const;

/**
 * The reasons a genuine link no longer works, in the API's own spelling
 * (sales.DeriveReAddressingState) and the catalog key each one reads as.
 */
export const RE_ADDRESSING_LINK_ENDED_KEYS = {
  withdrawn: "withdrawn",
  sale_reversed: "saleReversed",
  event_started: "eventStarted",
} as const;

export type ReAddressingLinkFailure =
  | (typeof RE_ADDRESSING_LINK_FAILURE_KEYS)[keyof typeof RE_ADDRESSING_LINK_FAILURE_KEYS]
  | (typeof RE_ADDRESSING_LINK_ENDED_KEYS)[keyof typeof RE_ADDRESSING_LINK_ENDED_KEYS];

/**
 * Which copy a refused link gets, from the API's error code and details.
 *
 * `RE_ADDRESSING_LINK_NO_LONGER_VALID` is the only code that carries a reason,
 * and a reason this app has not heard of falls back to "invalid" — the honest
 * floor, since the link does not work and the page has nothing truer to say. So
 * does a code it has not heard of at all.
 */
export function reAddressingLinkFailure(
  code: string | undefined,
  details?: unknown,
): ReAddressingLinkFailure {
  if (code === "RE_ADDRESSING_LINK_NO_LONGER_VALID") {
    const reason =
      typeof details === "object" && details !== null
        ? (details as { reason?: unknown }).reason
        : undefined;
    if (typeof reason === "string" && reason in RE_ADDRESSING_LINK_ENDED_KEYS) {
      return RE_ADDRESSING_LINK_ENDED_KEYS[reason as keyof typeof RE_ADDRESSING_LINK_ENDED_KEYS];
    }
    return "invalid";
  }
  if (code && code in RE_ADDRESSING_LINK_FAILURE_KEYS) {
    return RE_ADDRESSING_LINK_FAILURE_KEYS[code as keyof typeof RE_ADDRESSING_LINK_FAILURE_KEYS];
  }
  return "invalid";
}

/**
 * What the page renders, decided once from the address and the view read.
 *
 * `incomplete` is a link that arrived without its token — truncated by a mail
 * client, or copied by hand — and is told apart from a refusal because there is
 * nothing to send the API. `ready` and `accepted` both offer the button; the
 * second says the purchase is already theirs first. Every `failed` state is a
 * plain "this link no longer works" with the reason above and a way back to
 * the Storefront, since this reader has an ordinary account to go to rather
 * than a stranger's Ticket to wonder about.
 */
export type ReAddressingLinkState =
  | { kind: "incomplete" }
  | { kind: "ready"; view: ReAddressingLinkView }
  | { kind: "accepted"; view: ReAddressingLinkView }
  | { kind: "failed"; failure: ReAddressingLinkFailure };

/** The view read's outcome, as the page hands it over: the view, or the API's refusal. */
export type ReAddressingLinkRead =
  | { status: "ok"; view: ReAddressingLinkView }
  | { status: "error"; code: string | undefined; details?: unknown };

export function reAddressingLinkState(
  token: string,
  read: ReAddressingLinkRead | null,
): ReAddressingLinkState {
  if (token === "") {
    return { kind: "incomplete" };
  }
  if (!read || read.status === "error") {
    return { kind: "failed", failure: reAddressingLinkFailure(read?.code, read?.details) };
  }
  return read.view.accepted_at
    ? { kind: "accepted", view: read.view }
    : { kind: "ready", view: read.view };
}

/**
 * Where the browser goes after a successful press.
 *
 * With a session set, straight to the Sale in the Customer Area — the same
 * anchor the Confirmation Link and the checkout success page use, so the reader
 * lands on the purchase and not merely on the list. With consent outstanding,
 * to the sign-in page's consent step with that same Sale as the destination,
 * exactly where the Google callback sends a first-time Customer: one consent
 * step, one submission endpoint, and the sign-in page pushes them on to the
 * Sale when it is done.
 */
export function reAddressingLinkNext(accepted: ReAddressingAccepted): string {
  const sale = customerAreaSaleHref(accepted.ticket_sale_id);
  return accepted.consent_required ? signInConsentPath(sale) : sale;
}
