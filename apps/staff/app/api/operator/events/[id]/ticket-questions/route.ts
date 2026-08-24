import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorTicketQuestionRow } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// GET returns every Ticket Question of one Event for the Platform Operator, in
// every review state and retired ones included (#410, ADR 0056).
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorTicketQuestionRow[]>(
      `/api/v1/operator/events/${encodeURIComponent(id)}/ticket-questions`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
