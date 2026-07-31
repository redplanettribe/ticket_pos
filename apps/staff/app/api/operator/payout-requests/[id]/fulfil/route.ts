import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayoutFulfilment } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// POST records the Payout answering this request and marks the request paid, in
// one transaction on the API side. The operator's email is taken there from the
// session and stamps both the Payout's recorded_by and the request's
// resolved_by; nothing this proxy forwards can claim either.
//
// A 409 comes back when somebody answered the request first, and its message is
// load-bearing rather than decorative: NO Payout row survives a lost race, so an
// operator who also transferred the money must be told to record the Payout
// directly (ADR 0026). jsonFromAPIError passes the API's own message through
// untouched, which is exactly what this path needs.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorPayoutFulfilment>(
      `/api/v1/operator/payout-requests/${encodeURIComponent(id)}/fulfil`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope, { status: 201 });
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
