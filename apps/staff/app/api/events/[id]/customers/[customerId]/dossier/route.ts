import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; customerId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// One Customer's Dossier on one Event (#638). The API decides who may read it
// (Org Admin, Event Owner) and answers 404 for another Organization's Event or a
// Customer with nothing here; this route only attaches the session.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, customerId } = await context.params;

  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<unknown>(
        `/api/v1/staff/events/${encodeURIComponent(id)}/customers/${encodeURIComponent(customerId)}/dossier`,
        { method: "GET", sessionToken: token, signal },
      );
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
