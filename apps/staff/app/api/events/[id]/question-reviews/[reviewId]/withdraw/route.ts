import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; reviewId: string }>;
};

// Withdraws an outstanding Question Review; its questions return to draft
// (#406, ADR 0056).
export async function POST(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, reviewId } = await context.params;

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/question-reviews/${reviewId}/withdraw`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
