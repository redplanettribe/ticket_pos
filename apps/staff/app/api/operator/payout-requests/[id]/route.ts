import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayoutRequestDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// GET returns one payout request in full — the snapshot bank details included —
// with its organization and that organization's live balances. This is the only
// operator payload carrying a whole account number, and it carries exactly one.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorPayoutRequestDetail>(
      `/api/v1/operator/payout-requests/${encodeURIComponent(id)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
