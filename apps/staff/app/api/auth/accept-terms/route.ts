import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { PENDING_TERMS_COOKIE, clearedPendingTermsCookieOptions } from "@/lib/pending-terms";
import { SESSION_COOKIE_NAME, sessionCookieOptions } from "@/lib/session";

type AcceptTermsData = {
  session_id: string;
  session: {
    email: string;
    memberships: Array<{ member_id: string }>;
    active_member: { member_id: string } | null;
  };
  terms_required: unknown | null;
};

/**
 * Finishes a staff sign-in the terms gate held (#538, ADR 0066).
 *
 * The pending-terms token arrives in the body on the passcode path — the form
 * holds it from the verify response — and in the httpOnly cookie on the Google
 * path, where the callback had no page to hand it to. Body wins when both are
 * present, for the verify route's reason: a client that names one is stating
 * something. Either way the cookie is cleared: the token is single-use on the
 * API, so keeping a copy after the attempt is a stale credential and nothing
 * else.
 *
 * On success the session cookie is written exactly as verify-otp writes it,
 * because the API minted exactly the session a verify would have.
 */
export async function POST(request: Request) {
  try {
    const body = (await request.json().catch(() => ({}))) as {
      pending_terms_token?: string;
      terms_acceptance?: boolean;
      adulthood_declaration?: boolean;
    };
    const cookieStore = await cookies();
    const token = body.pending_terms_token || cookieStore.get(PENDING_TERMS_COOKIE)?.value || "";

    const envelope = await callBackend<AcceptTermsData>("/api/v1/auth/terms/accept", {
      method: "POST",
      body: JSON.stringify({
        pending_terms_token: token,
        terms_acceptance: body.terms_acceptance === true,
        // The Adulthood Declaration (#587, ADR 0069), relayed as given and
        // never upgraded: an answer this route does not have is false, and
        // false where the pinned edition asks is refused by the API before a
        // session is minted. The API ignores it where the edition does not ask.
        adulthood_declaration: body.adulthood_declaration === true,
      }),
    });

    cookieStore.set(PENDING_TERMS_COOKIE, "", clearedPendingTermsCookieOptions());
    if (envelope.data?.session_id) {
      cookieStore.set(SESSION_COOKIE_NAME, envelope.data.session_id, sessionCookieOptions());
    }

    return NextResponse.json(envelope);
  } catch (error) {
    // Whatever happened, the token is spent or worthless: the stale copy must
    // not outlive the attempt.
    try {
      (await cookies()).set(PENDING_TERMS_COOKIE, "", clearedPendingTermsCookieOptions());
    } catch {
      // Clearing a cookie must never mask the real error below.
    }
    return handleAPIError(error);
  }
}

function handleAPIError(error: unknown) {
  if (error && typeof error === "object" && "status" in error && "code" in error) {
    const apiError = error as {
      status: number;
      code: string;
      message: string;
      requestId: string;
      details?: unknown;
    };
    return NextResponse.json(
      {
        data: null,
        error: {
          code: apiError.code,
          message: apiError.message,
          details: apiError.details,
        },
        request_id: apiError.requestId,
      },
      { status: apiError.status },
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
