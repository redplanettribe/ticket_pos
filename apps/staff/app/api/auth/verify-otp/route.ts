import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { PENDING_TERMS_COOKIE, pendingTermsCookieOptions } from "@/lib/pending-terms";
import { detectedStaffLocale } from "@/lib/request-locale";
import { SESSION_COOKIE_NAME, sessionCookieOptions } from "@/lib/session";

type VerifyData = {
  session_id: string;
  session: {
    email: string;
    memberships: Array<{ member_id: string }>;
    active_member: { member_id: string } | null;
  } | null;
  terms_required: { pending_terms_token: string } | null;
};

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<VerifyData>("/api/v1/auth/otp/verify", {
      method: "POST",
      /*
        The detected language rides in on the verify body, and this is the
        frontend half of the flow whose backend half shipped in #284: the API
        records it as the Staff Locale IF the person has none, and never
        overwrites one they stated (ADR 0041). That is what makes their first
        one-time passcode and every screen after it agree without their visiting
        a setting.

        Detected HERE and not in the browser: the ladder reads Accept-Language,
        which is a request header the page cannot see, and the cookie, which the
        server has anyway. The client sends the email and the code; the language
        is an observation about the request, so the request handler makes it.

        Spread last-but-one so a client that ever does send `locale` wins — there
        is no such caller today, and if one appears it is stating something rather
        than being detected.
      */
      body: JSON.stringify({ locale: await detectedStaffLocale(), ...body }),
    });

    const cookieStore = await cookies();
    // A gated verify minted nothing (#538): no session cookie to write. The
    // token also rides the pending-terms cookie so the accept route can read it
    // server-side — the same fallback the Google door depends on.
    if (envelope.data?.terms_required) {
      cookieStore.set(
        PENDING_TERMS_COOKIE,
        envelope.data.terms_required.pending_terms_token,
        pendingTermsCookieOptions(),
      );
    } else if (envelope.data?.session_id) {
      cookieStore.set(SESSION_COOKIE_NAME, envelope.data.session_id, sessionCookieOptions());
    }

    return NextResponse.json(envelope);
  } catch (error) {
    return handleAPIError(error);
  }
}

function handleAPIError(error: unknown) {
  if (error && typeof error === "object" && "status" in error && "code" in error) {
    const apiError = error as { status: number; code: string; message: string; requestId: string; details?: unknown };
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
