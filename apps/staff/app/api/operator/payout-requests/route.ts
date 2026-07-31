import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayoutRequestQueuePage } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns the cross-organization queue of outstanding payout requests,
// oldest first, in the ADR-0006 nested { data, pagination } envelope, forwarding
// the pagination query string to the Go API. Account numbers arrive masked from
// the API — this proxy never sees a whole one.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<OperatorPayoutRequestQueuePage>(
      `/api/v1/operator/payout-requests${search}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
