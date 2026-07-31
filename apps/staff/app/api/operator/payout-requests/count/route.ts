import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPendingPayoutRequestCount } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns how many payout requests are outstanding platform-wide — the
// badge the operator navigation wears. Its own route rather than a field on the
// summary, because it is read on every page an operator opens.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorPendingPayoutRequestCount>(
      "/api/v1/operator/payout-requests/count",
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
