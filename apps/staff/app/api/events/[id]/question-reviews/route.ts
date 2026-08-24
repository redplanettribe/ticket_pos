import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// Submits the Event's draft Ticket Questions as one Question Review (#406,
// ADR 0056): a note, and the acknowledgement of what is being collected.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/question-reviews`, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope, { status: 201 });
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
