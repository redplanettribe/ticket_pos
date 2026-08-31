import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { StaffLegalRecord } from "@/lib/legal-records";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ digest: string }>;
};

// One staff person's Terms Acceptance record (#566, spec #556, ADR 0067).
//
// KEYED ON A STAFF DIGEST, because a staff person has no id — the person key of
// the Staff platform is an email — and an address must never reach a URL. The
// digest is 32 hex characters and stands for the person; the API resolves it by
// MATCHING across the staff population, because the digest is one-way and there
// is deliberately no reverse.
//
// A thin proxy. The API answers 404 for a digest that matches nobody and 503 on
// a deployment with no link secret, where the match would resolve to an
// arbitrary person — the wrong record rather than a degraded one.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { digest } = await context.params;
  try {
    const envelope = await callBackend<StaffLegalRecord>(
      `/api/v1/operator/legal/staff/${encodeURIComponent(digest)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
