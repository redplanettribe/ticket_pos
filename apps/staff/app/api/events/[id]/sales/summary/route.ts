import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// GET returns the Event's Net Proceeds stat strip figures. The Go API restricts
// it to Org Admins and Event Owners, so an Event Staff request is refused there
// (403) rather than here.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/sales/summary`, {
        method: "GET",
        sessionToken: token,
        signal,
      });
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
