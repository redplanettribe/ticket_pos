import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorSaleReversal } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ confirmationRef: string }>;
};

// POST records an Operator Reversal: the operator refunded the buyer
// off-platform, and this marks the sale reversed. The API never calls the
// Payment Provider on this path — the money has already moved — and it stamps
// the record with the operator's session email, so nothing about who asserted
// it travels in the body. Refused for anyone not on the platform operator
// allowlist (ADR 0015).
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { confirmationRef } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorSaleReversal>(
      `/api/v1/operator/sales/${encodeURIComponent(confirmationRef)}/reverse`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
