import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { LegalCustomerRecord } from "@/lib/legal-records";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ customerId: string }>;
};

// One Customer's consent record (#566, spec #556, ADR 0067).
//
// KEYED ON THE OPAQUE UUID, which is the whole reason this route exists and the
// one it replaces — GET /api/operator/customers/{email}/consent — is deleted. A
// URL is written to the reverse proxy's access log, kept in browser history and
// sent onward in the Referer header of the next click, and a data subject's
// email must reach none of those (#565).
//
// A thin proxy, like every route in here: the API owns every rule, including
// the one that matters most on this surface — only a Platform Operator may ask,
// because Customer identity is global and separate from staff (ADR 0010) and a
// Customer's consents belong to no Organization. Nothing here checks that, and
// nothing here should: a check duplicated in a BFF is a check that can disagree.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { customerId } = await context.params;
  try {
    const envelope = await callBackend<LegalCustomerRecord>(
      `/api/v1/operator/legal/customers/${encodeURIComponent(customerId)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
