import { NextResponse } from "next/server";

import { beginCheckout, type BeginCheckoutRequest } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { rememberCheckoutContext } from "@/lib/checkout-context";

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

  try {
    const result = await beginCheckout(orgSlug, eventSlug, {
      customer_email: asTrimmedString(body.customer_email),
      customer_first_name: asTrimmedString(body.customer_first_name),
      customer_last_name: asTrimmedString(body.customer_last_name),
      lines,
    });

    // Remembered only once the API accepted the checkout: the slugs were just
    // validated against SLUG_PATTERN, so the path is safe to become an href on
    // the terminal pages.
    await rememberCheckoutContext({
      clientTransactionId: result.client_transaction_id,
      eventPath: `/${orgSlug}/events/${eventSlug}`,
      eventName: asTrimmedString(body.event_name).slice(0, 200),
    });

    return NextResponse.json({ data: result, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
