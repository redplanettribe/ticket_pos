/**
 * Envelope helpers shared by the Storefront's Customer route handlers.
 *
 * Every one of those handlers is a BFF hop: the browser calls it, it calls the
 * Go API, and it returns what the API said (ADR 0008). Keeping the relay in one
 * place is what makes the API's verdict reach the browser intact by construction
 * rather than per route.
 *
 * `code` travels beside `message` for a reason that is not cosmetic: the surface
 * that renders this picks its own words from the code and keeps the message only
 * as the fallback for a code it does not know (ADR 0022). A relay that dropped
 * the code would leave every Storefront failure stuck in English.
 */

import { NextResponse } from "next/server";

import { APIError } from "./api";
import type { SessionOutcome } from "./customer-session";

/** Relays a failed API call to the browser with the API's own code and message. */
export function apiErrorResponse(error: unknown): NextResponse {
  if (error instanceof APIError) {
    return NextResponse.json(
      {
        data: null,
        error: { code: error.code, message: error.message, details: error.details },
        request_id: error.requestId,
      },
      { status: error.status },
    );
  }
  return NextResponse.json(
    {
      data: null,
      error: { code: "INTERNAL_ERROR", message: "Unexpected error" },
      request_id: crypto.randomUUID(),
    },
    { status: 500 },
  );
}

/** The response for a browser call made without a Customer Session cookie. */
export function notSignedInResponse(): NextResponse {
  return NextResponse.json(
    {
      data: null,
      error: { code: "CUSTOMER_SESSION_NOT_FOUND", message: "Not signed in" },
      request_id: crypto.randomUUID(),
    },
    { status: 401 },
  );
}

/**
 * Relays a session-scoped read to the browser: the three SessionOutcome cases in
 * the one place that decides what each of them looks like on the wire.
 *
 * Every /api/customer read handler is the same relay — call the API through the
 * session, hand back what it said — so they share this rather than each carrying
 * a copy of the switch. Signed-out is deliberately not an error: a session that
 * expired or was signed out is reported as "not signed in", because the caller's
 * recovery is to offer sign-in (PRD user story 37). A transport failure is a 502
 * so it can never be mistaken for either of the other two.
 *
 * Only the read handlers belong here. The passcode-verify and Confirmation-Link
 * paths set cookies and shape their own bodies, so they relay through
 * apiErrorResponse instead.
 */
export function sessionOutcomeResponse<T>(outcome: SessionOutcome<T>): NextResponse {
  switch (outcome.status) {
    case "ok":
      return NextResponse.json({
        data: outcome.data,
        error: null,
        request_id: crypto.randomUUID(),
      });
    case "signed-out":
      return notSignedInResponse();
    default:
      return NextResponse.json(
        {
          data: null,
          error: { code: "INTERNAL_ERROR", message: outcome.message },
          request_id: crypto.randomUUID(),
        },
        { status: 502 },
      );
  }
}
