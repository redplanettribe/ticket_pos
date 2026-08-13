import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { consentEvidenceHeaders } from "@/lib/consent-evidence";

/**
 * Submits a Consent Withdrawal made on Proof of Email Ownership alone (#270,
 * parent #265, ADR 0039).
 *
 * IT IS A THIN PROXY AND THE API OWNS EVERY RULE. Which submissions may skip
 * Policy Acceptance, whether a submission grants anything, what a withdrawal
 * records and who is written to afterwards are all decided on the far side; this
 * relays a body and an envelope. The one thing it does that a fetch from the
 * page could not is carry the request's evidence of itself — the client address
 * as this process derives it, the browser's user agent, and the page the act
 * happened on — because the API never sees a browser (ADR 0008).
 *
 * THE BODY IT SENDS CAN ONLY DENY. The page below composes `false` for each
 * consent being taken away and omits the rest, and this route relays it
 * unchanged rather than "helpfully" filling in the missing fields: an omitted
 * consent means "not on this submission" and is left exactly as it stands, so a
 * default written here would withdraw something nobody asked to withdraw. The
 * API refuses anything that is not a denial on this path anyway — that is the
 * guarantee, and this is the surface.
 *
 * IT SETS NO COOKIE. There is no session to take custody of: the API mints none
 * for this act, on purpose, and the person is as signed out afterwards as they
 * were before. It shares an endpoint with the sign-in consent submission, whose
 * route DOES take custody of a session — which is exactly why this is a separate
 * file rather than a branch inside that one.
 */
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<{
      withdrawal: {
        marketing_consent: string;
        networking_consent: string;
        withdrew: { marketing_consent: boolean; networking_consent: boolean };
      } | null;
    }>("/api/v1/customer/auth/consent", {
      method: "POST",
      body: JSON.stringify(body),
      headers: consentEvidenceHeaders(request.headers),
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
