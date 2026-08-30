import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// The Event's Holder List (#333; the Outstanding Answers read of #313, widened
// to the roster): every Ticket of the Event, who is coming on each, and — where
// the Event asks questions — what each still owes. The path keeps its
// historical name.
//
// The paging parameters — and the `outstanding` filter — are FORWARDED AND NOT
// PARSED. The API floors, clamps and defaults them itself, and a second reading
// of them here could only ever disagree with the first — the BFF's job on this
// route is to attach the session and get out of the way.
//
// Nothing here knows about the feature flags either. The API answers 404 to
// this while both TICKET_ASSIGNMENT_ENABLED and TICKET_QUESTIONS_ENABLED are
// off (ADR 0045), and one answer to whether a feature is on is the whole point
// of the staff app never reading an env var.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const query = new URL(request.url).searchParams.toString();
  const suffix = query ? `?${query}` : "";

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/outstanding-answers${suffix}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
