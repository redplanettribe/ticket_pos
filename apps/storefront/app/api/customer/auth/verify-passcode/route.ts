import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import {
  CUSTOMER_SESSION_COOKIE,
  customerSessionCookieOptions,
  type CustomerVerifyResult,
} from "@/lib/customer-session";

/**
 * Verifies a passcode and takes custody of the resulting Customer Session token.
 *
 * The token the API mints here is the credential for everything the Customer
 * Area shows, and it is written straight into an httpOnly cookie without ever
 * being handed to page scripts. The response body deliberately drops
 * `session_id` and returns only the session view, so nothing a browser can read
 * carries the token.
 *
 * The request body is relayed as it arrives, which is how the form's `locale`
 * reaches the API to be remembered as the Customer's Digest Locale (ADR 0030).
 * This route has no address of its own to read one from — every route under
 * /api is unprefixed — so the page that names its language is the one that
 * sends it.
 */
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<CustomerVerifyResult>(
      "/api/v1/customer/auth/otp/verify",
      { method: "POST", body: JSON.stringify(body) },
    );

    const result = envelope.data;

    // The consent step (#251): the passcode was right and no session exists yet,
    // because this Customer has not accepted the Policy Version in effect. NO
    // COOKIE IS WRITTEN — there is no session to hold custody of, and writing
    // one here is exactly the bug the gate exists to prevent. The token goes to
    // the form, which shows the Short Notice and the boxes and exchanges it at
    // /api/customer/auth/consent. A visitor who closes the tab here stays signed
    // out, which is the correct outcome of declining.
    if (result?.consent_required) {
      return NextResponse.json({
        data: { session: null, consent_required: result.consent_required },
        error: null,
        request_id: envelope.request_id ?? crypto.randomUUID(),
      });
    }

    if (!result?.session_id) {
      return NextResponse.json(
        {
          data: null,
          error: { code: "INTERNAL_ERROR", message: "Sign-in did not return a session" },
          request_id: envelope.request_id ?? crypto.randomUUID(),
        },
        { status: 500 },
      );
    }

    const store = await cookies();
    store.set(CUSTOMER_SESSION_COOKIE, result.session_id, customerSessionCookieOptions());

    return NextResponse.json({
      data: { session: result.session, consent_required: null },
      error: null,
      request_id: envelope.request_id ?? crypto.randomUUID(),
    });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
