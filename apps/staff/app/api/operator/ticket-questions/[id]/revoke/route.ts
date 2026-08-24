import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import type { TicketQuestion } from "@/lib/ticket-questions";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// POST is the Revocation (#410, ADR 0056): retires an approved Ticket Question
// with a reason the Organization is told. The reason is required by the API,
// which refuses a blank one with a field error; the Answers already given stay.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<TicketQuestion>(
      `/api/v1/operator/ticket-questions/${encodeURIComponent(id)}/revoke`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
