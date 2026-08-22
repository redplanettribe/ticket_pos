import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; ticketId: string; questionId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// One Ticket's Answer to one Ticket Question (#310).
//
// Nothing here validates the body. Every rule about an Answer needs the
// question's KIND, which only the API has — text is valid for two kinds and
// refused by five — so a check here would be a second copy of the rule that
// could disagree with the first. What comes back on a refusal carries the kind
// and a problem token, which the dialog turns into a sentence.

// PUT and not POST: there is exactly one Answer per (Ticket, question) and the
// address names it, so answering and correcting are the same request.
export async function PUT(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketId, questionId } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/tickets/${ticketId}/answers/${questionId}`,
      { method: "PUT", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// A REAL DELETE, and the only one in this feature — not an exception to
// "retired, never deleted". A Ticket Question and an Option are the
// Organization's own words that other Tickets' Answers still point at, whereas
// an Answer points at nothing: removing one restores the state the Ticket was in
// before anybody answered.
export async function DELETE(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketId, questionId } = await context.params;

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/tickets/${ticketId}/answers/${questionId}`,
      { method: "DELETE", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
