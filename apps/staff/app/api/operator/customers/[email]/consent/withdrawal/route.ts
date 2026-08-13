import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorCustomerConsent } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ email: string }>;
};

// POST records a Consent Withdrawal that arrived off-platform — a posted form,
// or an email to the data-protection address (#271).
//
// The body is forwarded unread, as every proxy here forwards one. That includes
// the withdraw-only rule: the API refuses a body that tries to GRANT a consent,
// and it refuses it in the platform's single consent-write path rather than at
// any surface, so a check added here would be decoration. Who recorded the act
// is taken from the session by the API and can never travel in this body.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { email } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorCustomerConsent>(
      `/api/v1/operator/customers/${encodeURIComponent(email)}/consent/withdrawal`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
