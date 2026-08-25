import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorSaleReAddressing } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ confirmationRef: string }>;
};

// POST records a Sale Re-addressing: the address the buyer meant, which the API
// mails a Re-addressing Link. Nothing moves until that address accepts (ADR
// 0058), no Payment Provider is called, and the API stamps the record with the
// operator's session email, so nothing about who recorded it travels in the
// body. The response carries no link and no token — the link is the corrected
// address's alone. Refused for anyone not on the platform operator allowlist
// (ADR 0015).
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { confirmationRef } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorSaleReAddressing>(
      `/api/v1/operator/sales/${encodeURIComponent(confirmationRef)}/re-address`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// DELETE withdraws the pending Sale Re-addressing (#423): the record is kept
// and stamped withdrawn, its link stops working, and nobody is emailed. No
// body. Refused when nothing is pending, and for anyone not on the platform
// operator allowlist.
export async function DELETE(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { confirmationRef } = await context.params;
  try {
    const envelope = await callBackend<OperatorSaleReAddressing>(
      `/api/v1/operator/sales/${encodeURIComponent(confirmationRef)}/re-address`,
      { method: "DELETE", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
