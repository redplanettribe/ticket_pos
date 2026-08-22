import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; ticketSaleId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// One Ticket Sale's Tickets, each with its Ticket Questions and Answers (#310).
//
// The Ticket Sale is how staff reach a Ticket at all: it is the thing already in
// front of them when somebody calls about their order. Nothing here knows about
// the feature flag — the API answers 404 to this while it is off (ADR 0045), and
// a second copy of that decision in the BFF could disagree with it.
export async function GET(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketSaleId } = await context.params;

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-sales/${ticketSaleId}/tickets`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
