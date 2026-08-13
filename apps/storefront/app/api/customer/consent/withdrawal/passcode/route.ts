import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { clientIpHeaders } from "@/lib/client-ip";

/**
 * Redeems a passcode for the proof that buys a Consent Withdrawal (#270,
 * parent #265, ADR 0039).
 *
 * IT SETS NO COOKIE, AND THAT IS THE WHOLE DIFFERENCE from the verify route it
 * otherwise resembles. The API answers this door with a short-lived, single-use
 * pending-consent token and never with a Customer Session, so there is nothing
 * here to take custody of: a person proving their address in order to exercise a
 * right must not be left signed in on a machine they borrowed. Nothing in this
 * file touches `cookies()`, which is the property to preserve — a session cookie
 * set here would silently undo the guarantee the endpoint was built to make.
 *
 * The token is handed back to the page, deliberately, exactly as the sign-in
 * consent step's is: it is spent within minutes by the very next request, it can
 * only buy a withdrawal, and a token in an httpOnly cookie would have to be
 * carried across a request this app cannot attribute to the person who started
 * it.
 */
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<{
      pending_consent_token: string;
      expires_at: string;
    }>("/api/v1/customer/consent/withdrawal/passcode/verify", {
      method: "POST",
      body: JSON.stringify(body),
      // The client address as this process derives it, never the browser's own
      // copy of the forwarding chain (ADR 0008).
      headers: clientIpHeaders(request.headers),
    });

    return NextResponse.json({
      data: envelope.data,
      error: envelope.error,
      request_id: envelope.request_id,
    });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
