import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { clientIpHeaders } from "@/lib/client-ip";
import {
  CUSTOMER_SESSION_COOKIE,
  customerSessionCookieOptions,
  type CustomerVerifyResult,
} from "@/lib/customer-session";
import {
  PENDING_CONSENT_COOKIE,
  clearedPendingConsentCookieOptions,
} from "@/lib/pending-consent";

/**
 * Finishes a sign-in that was held for consent: relays the answers and takes
 * custody of the Customer Session the API mints on the far side of them (#251).
 *
 * It is the verify route's twin and behaves like it in the one way that
 * matters: the session token goes straight from this process into an httpOnly
 * cookie and is never handed to page scripts. What differs is what is being
 * exchanged — a pending-consent token and three answers, rather than a passcode.
 *
 * THREE HEADERS ARE FORWARDED, and they are the request's evidence of itself.
 * The API records the circumstances of every capture act — IP, user agent,
 * origin — and it cannot observe any of them: no browser reaches it directly
 * (ADR 0008), so what it would otherwise see is this process. The IP is derived
 * the same way the passcode route derives it, from the forwarding chain and
 * never from the browser's own copy; the user agent and the referring page are
 * the browser's own headers, relayed verbatim. None of the three proves
 * anything — what proves is the passcode or the Google token already spent —
 * and all three are what a compliance officer needs to tie a record to a
 * moment.
 *
 * The answers themselves are relayed as they arrive. They are not this app's to
 * interpret: whether an unticked box is a No, which boxes this Customer was
 * owed, and whether the submission may proceed at all are the API's rulings,
 * and a Storefront that pre-judged any of them would be a second place for the
 * consent rules to live.
 */
export async function POST(request: Request) {
  const store = await cookies();
  // The held Google Sign-In's transport, spent the moment a submission is made
  // (#252). It is erased before the API is called and on every branch below,
  // because what makes it worthless is the submission being ATTEMPTED, not the
  // submission succeeding: the API consumes the pending-consent token before it
  // judges the answers, so a refused submission leaves a token that will never
  // work again. Leaving the cookie behind would offer that dead token to the
  // next render of the sign-in page.
  //
  // The passcode door sets no such cookie and this deletes nothing for it — a
  // request with no cookie is deleted just as cheaply, and one branch for both
  // doors is the point of them sharing this endpoint.
  store.set(PENDING_CONSENT_COOKIE, "", clearedPendingConsentCookieOptions());

  try {
    const body = await request.json();
    const envelope = await callBackend<CustomerVerifyResult>("/api/v1/customer/auth/consent", {
      method: "POST",
      body: JSON.stringify(body),
      headers: {
        ...clientIpHeaders(request.headers),
        "User-Agent": request.headers.get("user-agent") ?? "",
        Referer: request.headers.get("referer") ?? "",
      },
    });

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
