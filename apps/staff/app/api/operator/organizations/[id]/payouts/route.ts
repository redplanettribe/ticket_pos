import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorPayout } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// POST records a Payout against the Organization, stamped by the API with the
// operator's session email. An amount exceeding the Withdrawable Balance is
// accepted (ADR 0015); the dashboard confirms it before getting here.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorPayout>(
      `/api/v1/operator/organizations/${id}/payouts`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope, { status: 201 });
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
