import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * Records the acceptance the navigation gate asked for (#570, ADR 0067).
 *
 * The session token never leaves the server: the cookie is httpOnly, so the
 * interstitial's form posts here and this relays it with the Bearer credential,
 * exactly as every other signed-in write in this app does.
 *
 * IT TOUCHES NO COOKIE. Unlike /api/auth/accept-terms — the sign-in door's
 * counterpart, which writes the session cookie because the API minted a session
 * — nothing is minted here, nothing re-minted and nothing revoked. The person
 * was signed in before this request and is signed in after it, with the same
 * session row, and no passcode was spent. A response that quietly rewrote the
 * session cookie would be exactly the re-mint this ticket rules out.
 */
export async function POST(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return NextResponse.json(
      {
        data: null,
        error: { code: "UNAUTHORIZED", message: "Not signed in" },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    const body = (await request.json().catch(() => ({}))) as {
      gate_token?: string;
      terms_acceptance?: boolean;
      adulthood_declaration?: boolean;
    };

    const envelope = await callBackend<{ accepted: boolean }>("/api/v1/staff/terms/accept", {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify({
        gate_token: body.gate_token ?? "",
        // The API refuses an unticked box; this relays the answer as given and
        // never upgrades it.
        terms_acceptance: body.terms_acceptance === true,
        // The Adulthood Declaration beside it (#587, ADR 0069), relayed under
        // exactly the same rule: an answer this relay does not have is `false`,
        // and false where the pinned edition asks is the API's refusal to make,
        // never this route's to soften. Sent unconditionally because the API
        // ignores it wherever the edition does not ask — the edition decides
        // what is owed, and this relay decides nothing.
        adulthood_declaration: body.adulthood_declaration === true,
      }),
    });
    return NextResponse.json(envelope);
  } catch (error) {
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
