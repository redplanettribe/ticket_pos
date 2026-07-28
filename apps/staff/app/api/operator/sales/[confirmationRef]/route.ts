import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorSaleLookup } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ confirmationRef: string }>;
};

// GET returns the one Ticket Sale carrying a Sale Confirmation reference, from
// whichever Organization it belongs to. The reference is all the operator has,
// so it is the whole of the path; the API refuses anyone not on the platform
// operator allowlist (ADR 0015).
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { confirmationRef } = await context.params;
  try {
    const envelope = await callBackend<OperatorSaleLookup>(
      `/api/v1/operator/sales/${encodeURIComponent(confirmationRef)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
