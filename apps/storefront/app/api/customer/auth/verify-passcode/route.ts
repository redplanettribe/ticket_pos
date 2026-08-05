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
 */
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<CustomerVerifyResult>(
      "/api/v1/customer/auth/otp/verify",
      { method: "POST", body: JSON.stringify(body) },
    );

    const result = envelope.data;
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
      data: { session: result.session },
      error: null,
      request_id: envelope.request_id ?? crypto.randomUUID(),
    });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
