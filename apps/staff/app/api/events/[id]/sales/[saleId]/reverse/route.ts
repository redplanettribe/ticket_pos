import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; saleId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// POST reverses ONE imported Ticket Sale from the Sales list (#350, ADR 0050),
// forwarding to the Go endpoint and returning its JSON envelope. No body: there
// is no notify toggle, because the buyer of a reversed imported sale is mailed
// nothing — the Holders on it are told regardless. The browser talks only to
// the BFF; the session is attached here.
export async function POST(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, saleId } = await context.params;
  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/sales/${saleId}/reverse`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
