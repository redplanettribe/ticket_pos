import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { AFFILIATE_REF_COOKIE, readAffiliateCodes } from "@/lib/affiliate-ref";
import { beginCheckout, type BeginCheckoutRequest } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { rememberCheckoutContext } from "@/lib/checkout-context";
import { customerSessionToken } from "@/lib/customer-session";

// Begins a Payment and touches a cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The begin-checkout BFF hop: the browser posts the selection and checkout
 * identity here, this handler asks the Go API to begin the checkout
 * (server-side, per ADR 0008 — no browser may address the API), notes the
 * event page in the checkout-context cookie for the terminal pages, and hands
 * back the Payment Provider's redirect URL. The browser then performs a
 * full-page navigation to it: the payment page must be top-level, never an
 * iframe.
 *
 * Validation here is shape-only — is this parseable as a checkout at all? The
 * API owns the real rules (event published, capacity, email validity) and its
 * error envelope is relayed verbatim so the form can show the API's own
 * message and field details.
 */

/** Slugs come from our own URLs, but they are still browser input here. */
const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]*$/;

const MAX_LINES = 50;

type CheckoutRequestBody = {
  org_slug?: unknown;
  event_slug?: unknown;
  event_name?: unknown;
  customer_email?: unknown;
  customer_first_name?: unknown;
  customer_last_name?: unknown;
  customer_tax_id_type?: unknown;
  customer_tax_id_number?: unknown;
  customer_phone?: unknown;
  lines?: unknown;
};

function asTrimmedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
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

  try {
    // The Customer Session token, when the visitor has one, rides along in
    // Authorization. It is never required — guest checkout is the baseline — and
    // the API uses it for one thing only: deciding whether the Tax ID typed
    // below is the buyer's own assertion about themselves, and may therefore
    // become their stored one (ADR 0016). A dead or absent token simply checks
    // out as a guest, so nothing here treats its absence as a problem.
    const result = await beginCheckout(
      orgSlug,
      eventSlug,
      {
        customer_email: asTrimmedString(body.customer_email),
        customer_first_name: asTrimmedString(body.customer_first_name),
        customer_last_name: asTrimmedString(body.customer_last_name),
        customer_tax_id_type: asTrimmedString(body.customer_tax_id_type),
        customer_tax_id_number: asTrimmedString(body.customer_tax_id_number),
        ...(phone ? { customer_phone: phone } : {}),
        ...(affiliateCodes.length > 0 ? { affiliate_codes: affiliateCodes } : {}),
        lines,
      },
      await customerSessionToken(),
    );

    // Remembered only once the API accepted the checkout: the slugs were just
    // validated against SLUG_PATTERN, so the path is safe to become an href on
    // the terminal pages.
    // The buyer's address rides along so the success page can offer sign-in
    // already filled in (#121). Checkout is guest-facing, so this app's only
    // record of who bought is the form they just submitted; it is a prefill and
    // never a credential.
    await rememberCheckoutContext({
      clientTransactionId: result.client_transaction_id,
      eventPath: `/${orgSlug}/events/${eventSlug}`,
      eventName: asTrimmedString(body.event_name).slice(0, 200),
      customerEmail: asTrimmedString(body.customer_email),
    });

    return NextResponse.json({ data: result, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
