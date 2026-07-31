import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayoutRequestWhole } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// POST records that the bank sent the transfer back, with the reason the
// organizer reads. The reason is required by the API, which refuses a blank one.
//
// Nothing in the ledger is undone because nothing was ever written to it: the
// transfer lived on the request and never as a Payout, which is the point of the
// processing state. Only a `processing` request may fail, and failure is
// terminal — the organizer corrects their payout profile and asks again.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorPayoutRequestWhole>(
      `/api/v1/operator/payout-requests/${encodeURIComponent(id)}/failed`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
