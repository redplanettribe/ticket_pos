import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { AFFILIATE_REF_COOKIE, readAffiliateCodes } from "@/lib/affiliate-ref";
import { beginCheckoutSignedIn, type BeginCheckoutRequest } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { rememberCheckoutContext } from "@/lib/checkout-context";
import { checkoutLocaleFromReferer } from "@/lib/checkout-context-cookie";
import { consentEvidenceHeaders } from "@/lib/consent-evidence";
import { customerSessionToken } from "@/lib/customer-session";
import { isAppLocale, type AppLocale } from "@/lib/locale";
import { encodeSelection } from "@/lib/selection-url";

// Begins a Payment and touches a cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The begin-checkout BFF hop: the browser posts the selection and the buyer's
 * details here, this handler asks the Go API to begin the checkout
 * (server-side, per ADR 0008 — no browser may address the API), notes the
 * event page and the language it was being read in in the checkout-context
 * cookie for the return leg, and hands back the Payment Provider's redirect
 * URL. The browser then performs a full-page navigation to it: the payment page
 * must be top-level, never an iframe.
 *
 * IT POSTS NO EMAIL, BECAUSE THERE IS NOWHERE FOR ONE TO COME FROM (ADR 0054,
 * #385). The address a Ticket Sale is written to is read by the API off the
 * Customer Session token this hop forwards, so the whole class of mistake —
 * a sale addressed to a typo, to somebody else's inbox, to an address nobody
 * proved — is unspellable rather than merely guarded against. The dialog has no
 * email field to send one from, and a body that carried one anyway would be
 * ignored: nothing below reads it.
 *
 * The token is therefore REQUIRED here, and a request without one is refused
 * 401 without troubling the API. That is a courtesy and not the boundary — the
 * session-gated route refuses the same request itself, with the same code, and
 * a Confirmation Link session with 403 CUSTOMER_SESSION_SCOPE_INSUFFICIENT.
 * Enforcement belongs there because the BFF is a hop (ADR 0008).
 *
 * Validation here is otherwise shape-only — is this parseable as a checkout at
 * all? The API owns the real rules (event published, capacity, the buyer's
 * details) and its error envelope is relayed verbatim so the form can show the
 * API's own message and field details.
 */

/** Slugs come from our own URLs, but they are still browser input here. */
const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]*$/;

const MAX_LINES = 50;

type CheckoutRequestBody = {
  org_slug?: unknown;
  event_slug?: unknown;
  event_name?: unknown;
  // No customer_email. The address comes from the Customer Session and from
  // nothing a browser can put in a body (ADR 0054).
  customer_first_name?: unknown;
  customer_last_name?: unknown;
  customer_tax_id_type?: unknown;
  customer_tax_id_number?: unknown;
  customer_phone?: unknown;
  policy_acceptance?: unknown;
  marketing_consent?: unknown;
  networking_consent?: unknown;
  terms_acceptance?: unknown;
  adulthood_declaration?: unknown;
  lines?: unknown;
  locale?: unknown;
  answers?: unknown;
};

/**
 * The answer section, relayed shape-only and NEVER refused (#311, ADR 0044).
 *
 * This hop asks one question of each entry — is it addressable at all, meaning
 * does it name a Ticket Type, a ticket, and a question — and drops the ones that
 * are not, because an entry with no address is one the API could not act on
 * either. It asks NOTHING about the reply: which slot a question's kind takes,
 * whether an Option belongs to it, whether a number is a number, are all the
 * API's findings, and its answer to "no" is to drop the reply rather than refuse
 * the purchase.
 *
 * A malformed `answers` therefore yields an empty list and a completed checkout,
 * never a 400. That is the difference between this and parseLines above, and it
 * is the whole of ADR 0044 expressed in one function: a cart that cannot be read
 * is a checkout that cannot happen, while an answer that cannot be read is a
 * t-shirt size.
 *
 * The reply keys are copied verbatim rather than rebuilt, so `checked: false`
 * survives — an unticked box somebody read is an answer — and `number` stays the
 * string it was typed as, because rebuilding it through a JSON number is how a
 * NUMERIC column loses a trailing zero.
 */
function parseAnswers(value: unknown): NonNullable<BeginCheckoutRequest["answers"]> {
  if (!Array.isArray(value)) return [];
  const answers: NonNullable<BeginCheckoutRequest["answers"]> = [];
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) continue;
    const { ticket_type_id, ticket_index, ticket_question_id, text, number, date, checked, option_ids } =
      entry as Record<string, unknown>;
    if (typeof ticket_type_id !== "string" || ticket_type_id.trim() === "") continue;
    if (typeof ticket_question_id !== "string" || ticket_question_id.trim() === "") continue;
    if (typeof ticket_index !== "number" || !Number.isSafeInteger(ticket_index)) continue;
    answers.push({
      ticket_type_id: ticket_type_id.trim(),
      ticket_index,
      ticket_question_id: ticket_question_id.trim(),
      ...(typeof text === "string" ? { text } : {}),
      ...(typeof number === "string" ? { number } : {}),
      ...(typeof date === "string" ? { date } : {}),
      ...(typeof checked === "boolean" ? { checked } : {}),
      ...(Array.isArray(option_ids) && option_ids.every((id) => typeof id === "string")
        ? { option_ids: option_ids as string[] }
        : {}),
    });
  }
  return answers;
}

/**
 * One consent box, relayed only when the dialog actually drew it.
 *
 * `undefined` and `false` mean different things all the way down to the column
 * (migration 064): absent is "this box was not shown", false is a person who was
 * shown it and left it unticked, which is an explicit No. So anything that is
 * not a boolean is dropped rather than coerced — this hop is shape-only, and a
 * `null` from a client turning into a refusal would be this app answering on the
 * buyer's behalf.
 */
function consentAnswer(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function asTrimmedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/**
 * The language the buyer was reading in, which now has two readers: the
 * checkout-context cookie the return leg redirects by, and the API itself,
 * which records it on the Ticket Sale as its Sale Locale so that mail about
 * the sale is written in it (ADR 0033).
 *
 * ONE VALUE ANSWERS BOTH. The page a buyer read and the language their receipt
 * arrives in are the same fact, and resolving it twice would be a way for the
 * return page and the email to disagree about a checkout that happened once.
 * This is not content negotiation reaching the API: no read path takes an
 * Accept-Language, and what travels is a page reporting the language in its own
 * address (ADR 0027).
 *
 * The page that posted here states it, because it is the only party that knows
 * the answer for certain: it was rendered under a locale prefix. The Referer is
 * kept as the fallback for a client that has not been updated, but it is a
 * fragile signal to lead with — a Referrer-Policy header, a privacy extension
 * or a future browser default each drop it silently, and the buyer then comes
 * back from a real payment into the wrong language.
 *
 * Anything unrecognised is ignored rather than trusted: this is browser input,
 * it decides which of this Storefront's own prefixes the return leg redirects
 * into, and an unknown value would be a prefix no page is served under. Null
 * means nothing here has anything to say, which leaves the return leg exactly
 * the ordinary cookie-then-Accept-Language guess it had before.
 */
function checkoutLocale(value: unknown, referer: string | null): AppLocale | null {
  return isAppLocale(value) ? value : checkoutLocaleFromReferer(referer);
}

function parseLines(value: unknown): BeginCheckoutRequest["lines"] | null {
  if (!Array.isArray(value) || value.length === 0 || value.length > MAX_LINES) {
    return null;
  }
  const lines: BeginCheckoutRequest["lines"] = [];
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) return null;
    const { ticket_type_id, quantity } = entry as Record<string, unknown>;
    if (typeof ticket_type_id !== "string" || ticket_type_id.trim() === "") return null;
    if (typeof quantity !== "number" || !Number.isSafeInteger(quantity) || quantity <= 0) {
      return null;
    }
    lines.push({ ticket_type_id: ticket_type_id.trim(), quantity });
  }
  return lines;
}

function badRequest(message: string): NextResponse {
  return NextResponse.json(
    {
      data: null,
      error: { code: "VALIDATION_FAILED", message },
      request_id: crypto.randomUUID(),
    },
    { status: 400 },
  );
}

export async function POST(request: Request) {
  let body: CheckoutRequestBody;
  try {
    body = (await request.json()) as CheckoutRequestBody;
  } catch {
    return badRequest("The checkout request could not be read.");
  }

  const orgSlug = asTrimmedString(body.org_slug).toLowerCase();
  const eventSlug = asTrimmedString(body.event_slug).toLowerCase();
  if (!SLUG_PATTERN.test(orgSlug) || !SLUG_PATTERN.test(eventSlug)) {
    return badRequest("The checkout request could not be read.");
  }

  const lines = parseLines(body.lines);
  if (!lines) {
    return badRequest("Select at least one ticket before checking out.");
  }

  // Who this sale will be addressed to, and the one thing this hop now insists
  // on (ADR 0054). Refused HERE only so that a browser which lost its session
  // between opening the dialog and pressing pay gets a 401 it can act on rather
  // than a round trip; the API refuses the identical request with the identical
  // code, and it is the API's refusal that makes this true.
  const sessionToken = await customerSessionToken();
  if (!sessionToken) {
    return notSignedInResponse();
  }

  // The phone number is relayed only when the buyer actually gave one, and the
  // key is dropped rather than sent blank (#103). A blank would be this app
  // asserting a value on the buyer's behalf, and PayPhone — which is where this
  // ends up — prohibits static or fabricated cardholder data. Whether the number
  // is well-formed is not decided here: shape-only is this hop's whole remit,
  // the dialog checks it for instant feedback, and the API's verdict is the one
  // that counts.
  const phone = asTrimmedString(body.customer_phone);

  // Affiliate Attribution's second half (ADR 0022): the codes this browser's
  // recent clicks on THIS Event left behind, newest first, within the
  // Attribution Window. They come from the cookie rather than from the request
  // body, so a page script cannot claim credit for a link nobody clicked, and
  // the key is dropped when nothing is remembered — the ordinary, unattributed
  // checkout. Which of them still names a live Affiliate Link is the API's
  // verdict — it takes the first that does — and either way the buyer's
  // checkout proceeds identically.
  const affiliateCodes = readAffiliateCodes(
    (await cookies()).get(AFFILIATE_REF_COOKIE)?.value ?? null,
    orgSlug,
    eventSlug,
    Date.now(),
  );

  // Resolved once, before the checkout begins, because both the API request
  // below and the cookie written after it are statements about the same page.
  const locale = checkoutLocale(body.locale, request.headers.get("referer"));

  // The answer section. Never a reason to fail: an unreadable one is an empty
  // one, and the key is dropped rather than sent as [] so that a checkout with
  // nothing to say is byte-identical to the one before this feature existed.
  const answers = parseAnswers(body.answers);

  try {
    // The Customer Session token rides in Authorization, and it is now what
    // NAMES THE BUYER rather than a hint about them (ADR 0054, #384). The API
    // reads the address off it, writes the Ticket Sale to that address, and
    // treats the name, Tax ID and phone below as the buyer's own assertions
    // about themselves — which is what lets them become the stored ones
    // (ADR 0016) and what makes a consent given here an answer rather than a
    // Pending Confirmation (ADR 0035).
    const result = await beginCheckoutSignedIn(
      orgSlug,
      eventSlug,
      {
        customer_first_name: asTrimmedString(body.customer_first_name),
        customer_last_name: asTrimmedString(body.customer_last_name),
        customer_tax_id_type: asTrimmedString(body.customer_tax_id_type),
        customer_tax_id_number: asTrimmedString(body.customer_tax_id_number),
        ...(phone ? { customer_phone: phone } : {}),
        // The consent boxes (#253, #254). All three are relayed on identical
        // terms now, the required one included: present as sent — `false` and
        // all — and ABSENT when the dialog drew no such box, which since #254 is
        // what a Customer who has already accepted the current Policy Version
        // sends. It used to be coerced to `false` here, on the grounds that the
        // field was required on the wire; it is not required of everybody any
        // more, and a `false` this hop invented would be this app answering a
        // question nobody was asked.
        //
        // This hop deliberately enforces NOTHING: a second copy of a legal gate
        // is a second place for it to be wrong. The API recomputes which boxes
        // the buyer was owed and refuses with POLICY_ACCEPTANCE_REQUIRED where
        // one was owed and not given.
        ...(consentAnswer(body.policy_acceptance) !== undefined
          ? { policy_acceptance: consentAnswer(body.policy_acceptance) }
          : {}),
        ...(consentAnswer(body.marketing_consent) !== undefined
          ? { marketing_consent: consentAnswer(body.marketing_consent) }
          : {}),
        ...(consentAnswer(body.networking_consent) !== undefined
          ? { networking_consent: consentAnswer(body.networking_consent) }
          : {}),
        // The Terms box (#537, ADR 0066), relayed on exactly the terms of the
        // three above: present as sent when the dialog drew it, absent when it
        // did not, enforced by the API alone (TERMS_ACCEPTANCE_REQUIRED).
        ...(consentAnswer(body.terms_acceptance) !== undefined
          ? { terms_acceptance: consentAnswer(body.terms_acceptance) }
          : {}),
        // The Adulthood Declaration (#588, ADR 0069), relayed on exactly those
        // terms in turn: present as sent when the dialog drew the box, absent
        // when it did not. This hop enforces nothing here either — a second
        // copy of a legal gate is a second place for it to be wrong — and the
        // API answers a missing or unticked declaration with
        // ADULTHOOD_DECLARATION_REQUIRED, before any Payment exists.
        ...(consentAnswer(body.adulthood_declaration) !== undefined
          ? { adulthood_declaration: consentAnswer(body.adulthood_declaration) }
          : {}),
        ...(affiliateCodes.length > 0 ? { affiliate_codes: affiliateCodes } : {}),
        // Dropped rather than sent null when nothing here can say which page
        // this was: the API reads an absent language as "no page produced this
        // sale", which is the truth, and the receipt then falls back to what
        // the buyer's own record remembers (ADR 0033).
        ...(locale ? { locale } : {}),
        ...(answers.length > 0 ? { answers } : {}),
        lines,
      },
      sessionToken,
      // The technical proof of the consent captured on the dialog, forwarded the
      // way the sign-in consent route forwards it and for the same reason: the
      // API records the circumstances of a capture act and can observe none of
      // them from behind this hop. The IP comes from the forwarding chain and
      // never from a copy the browser could set; the other two are the browser's
      // own headers, verbatim. None of them proves anything — proof is a
      // Customer Session or nothing — and all of them are what ties a Consent
      // Record to a moment (#253).
      {
        ...consentEvidenceHeaders(request.headers),
      },
    );

    // Remembered only once the API accepted the checkout: the slugs were just
    // validated against SLUG_PATTERN, so the path is safe to become an href on
    // the terminal pages.
    //
    // WRITTEN IN FULL, BECAUSE A SESSION CAN END BETWEEN PAYING AND RETURNING
    // (#387). The buyer is about to leave this origin for the Payment Provider,
    // possibly for minutes; a cleared jar, a provider webview that keeps its own
    // cookies, a session revoked from another device or a return in a different
    // browser each land somebody who HAS ALREADY PAID on a terminal page with no
    // session at all. "Checkout requires a session, therefore the buyer will have
    // one when they come back" is the one inference this route may not make: it
    // is true of the request being handled here and says nothing about the one
    // that follows it.
    //
    // The language they were reading in rides along because the Event page that
    // called this route states it and this is the last moment anything knows it:
    // the Payment Provider's return URL is a locale-free constant, so without
    // this the handler behind it can only guess (T5). Null when neither the body
    // nor the Referer says, which leaves that handler exactly the guess it had
    // before — the same browser asks both times, so nothing is lost by not
    // writing one down here.
    await rememberCheckoutContext({
      clientTransactionId: result.client_transaction_id,
      eventPath: `/${orgSlug}/events/${eventSlug}`,
      eventName: asTrimmedString(body.event_name).slice(0, 200),
      // The basket, spelled the way a selection travels, so a Payment that is
      // declined can hand it back rather than an empty Event page (ADR 0054).
      // Rebuilt from the very lines the API just accepted rather than from
      // anything the browser said separately about them, so the cookie cannot
      // remember a different cart than the one that was charged for.
      selection: encodeSelection(
        Object.fromEntries(lines.map((line) => [line.ticket_type_id, line.quantity])),
      ),
      // The address this Ticket Sale was addressed to, AS THE API JUST REPORTED
      // IT — the address it read off the Customer Session, echoed back in the
      // begin-checkout response (#387). It is what lets a buyer whose session
      // died at the provider sign back into the Customer Area their new tickets
      // are actually in: a purchase made under one address and a session held
      // under another are different Customers (ADR 0011).
      //
      // TAKEN FROM THE API AND FROM NOTHING THE BROWSER SAID. This hop has no
      // other honest source: it forwards a session token it never reads, and an
      // address out of the request body would be `customer_email` back from the
      // dead, pointed the other way down the wire. It is a prefill either way —
      // the passcode still has to be proved — but a prefill this app invented
      // would be this app asserting who the buyer is.
      //
      // Blank only when the API said nothing, which is a Storefront running ahead
      // of an API that predates the field. parseCheckoutContext reads a blank one
      // as "no prefill" and the sign-in field simply arrives empty, exactly as it
      // does for a cookie minted by an older release.
      customerEmail: result.addressed_to ?? "",
      locale,
    });

    return NextResponse.json({ data: result, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
