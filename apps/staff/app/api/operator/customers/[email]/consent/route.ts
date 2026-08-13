import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorCustomerConsent } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ email: string }>;
};

// GET reports one Customer's consent state, found by email address (#271).
//
// A thin proxy, like every route in here: the API owns every rule, including
// the one that matters most on this surface — only a Platform Operator may ask,
// because Customer identity is global and separate from staff (ADR 0010) and a
// Customer's consents belong to no Organization. Nothing here checks that, and
// nothing here should: the allowlist is the API's and a check duplicated in a
// BFF is a check that can disagree.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { email } = await context.params;
  try {
    const envelope = await callBackend<OperatorCustomerConsent>(
      `/api/v1/operator/customers/${encodeURIComponent(email)}/consent`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
