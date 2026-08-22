import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; ticketTypeId: string; questionId: string; optionId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// Renaming an Option. The Option's identity is untouched, which is what keeps
// the Answers already given under the old wording attached to it.
export async function PATCH(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketTypeId, questionId, optionId } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-types/${ticketTypeId}/questions/${questionId}/options/${optionId}`,
      { method: "PATCH", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// DELETE retires and never deletes: a retired Option leaves new lists, stays on
// the Tickets that chose it, and keeps its column in the Sales Export.
export async function DELETE(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketTypeId, questionId, optionId } = await context.params;

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-types/${ticketTypeId}/questions/${questionId}/options/${optionId}`,
      { method: "DELETE", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
