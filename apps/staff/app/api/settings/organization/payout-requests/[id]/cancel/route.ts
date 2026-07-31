import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * Cancelling is the only change an Organization can make to an outstanding
 * Payout Request: it cannot be edited, only cancelled and re-asked (ADR 0026).
 * A POST to a verb rather than a DELETE, because nothing is deleted — the
 * request stays in the history saying it was asked for and withdrawn.
 */
type PayoutRequest = {
  id: string;
  status: string;
};

type RouteContext = {
  params: Promise<{ id: string }>;
};

export async function POST(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;

  try {
    const envelope = await callBackend<PayoutRequest>(
      `/api/v1/staff/organization/payout-requests/${id}/cancel`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
