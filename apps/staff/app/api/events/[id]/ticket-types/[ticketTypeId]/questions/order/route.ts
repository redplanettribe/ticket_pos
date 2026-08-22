import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; ticketTypeId: string }>;
};

// The whole running order of a Ticket Type's live Ticket Questions, stated at
// once rather than moved one index at a time — so two questions can never end up
// claiming the same place.
export async function PUT(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketTypeId } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-types/${ticketTypeId}/questions/order`,
      { method: "PUT", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
