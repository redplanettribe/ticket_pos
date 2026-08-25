import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import {
  CUSTOMER_SESSION_COOKIE,
  clearedCustomerSessionCookieOptions,
  customerSessionCookieOptions,
  type CustomerVerifyResult,
} from "@/lib/customer-session";
import {
  PENDING_CONSENT_COOKIE,
  encodePendingConsent,
  pendingConsentCookieOptions,
} from "@/lib/pending-consent";
import type { ReAddressingAccepted } from "@/lib/re-addressing-link";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Accepting a Sale Re-addressing (#422, parent #419, ADR 0058).
 *
 * A BFF hop like the Assignment Link's: the browser calls this, this calls the
 * Go API, and what the API said comes back intact (ADR 0008). It SENDS NO
 * SESSION TOKEN AND READS NO COOKIE for the same reason as that route — the
 * reader may have no account yet, and this call is what creates one.
 *
 * IT IS A POST BECAUSE IT WRITES, and the page above it deliberately reads the
 * view and stops. Mail security scanners open every link in every message
 * before a human sees it; a page that accepted on being fetched would move a
 * Sale to an address nobody proved. The press is the act.
 *
 * THE TOKEN TRAVELS IN THE BODY on this leg, so it reaches neither this app's
 * access log nor a Referer header. It asserts who somebody is.
 *
 * WHAT COMES BACK IS SET EXACTLY AS A PASSCODE SIGN-IN SETS IT. The API
 * answers on the passcode verify's own terms — a session, or a consent step —
 * and this route does with each what /api/customer/auth/verify-passcode and
 * the Google callback do: the session id goes into the httpOnly cookie and
 * never into the body; a pending consent goes into the pending-consent cookie
 * so the sign-in page's consent step can finish it, and any Customer Session
 * that was already in the jar is cleared, because the Sale has ALREADY MOVED
 * to the proven address and a stale session for somebody else would carry the
 * reader past the consent step into the wrong Customer Area.
 */
export async function POST(request: Request) {
  let body: Record<string, unknown>;
  try {
    body = (await request.json()) as Record<string, unknown>;
  } catch {
    return NextResponse.json(
      {
        data: null,
        error: { code: "INVALID_JSON", message: "We couldn't read that." },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  const token = body.token;
  if (typeof token !== "string" || token.trim() === "") {
    return NextResponse.json(
      {
        data: null,
        error: { code: "RE_ADDRESSING_LINK_INVALID", message: "This link is not valid." },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    const envelope = await callBackend<CustomerVerifyResult & { ticket_sale_id: string }>(
      "/api/v1/public/re-addressing-link",
      { method: "POST", body: JSON.stringify({ token }) },
    );
    const result = envelope.data;
    const requestId = envelope.request_id ?? crypto.randomUUID();
    const store = await cookies();

    if (result?.consent_required) {
      store.set(CUSTOMER_SESSION_COOKIE, "", clearedCustomerSessionCookieOptions());
      store.set(
        PENDING_CONSENT_COOKIE,
        encodePendingConsent(result.consent_required),
        pendingConsentCookieOptions(),
      );
      const accepted: ReAddressingAccepted = {
        ticket_sale_id: result.ticket_sale_id,
        consent_required: true,
      };
      return NextResponse.json({ data: accepted, error: null, request_id: requestId });
    }

    if (!result?.session_id) {
      return NextResponse.json(
        {
          data: null,
          error: { code: "INTERNAL_ERROR", message: "Accepting did not return a session" },
          request_id: requestId,
        },
        { status: 500 },
      );
    }

    store.set(CUSTOMER_SESSION_COOKIE, result.session_id, customerSessionCookieOptions());
    const accepted: ReAddressingAccepted = {
      ticket_sale_id: result.ticket_sale_id,
      consent_required: false,
    };
    return NextResponse.json({ data: accepted, error: null, request_id: requestId });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
