import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayoutRequestFull } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// POST records that the operator submitted the bank transfer and cannot yet
// confirm it. NO Payout is written — a Payout is money that moved, and this is
// money that has been sent (ADR 0014, ADR 0026 amendment) — and the request
// stays outstanding, keeping the organization's single slot while it is in
// flight.
//
// The body carries at most an optional transfer_reference. Who submitted it and
// when are the API's, taken from the staff session and its own clock, and are
// not forwarded from here: the instant is the one the 72-hour stale flag is
// measured against, and a caller must not be its source.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorPayoutRequestFull>(
      `/api/v1/operator/payout-requests/${encodeURIComponent(id)}/processing`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
