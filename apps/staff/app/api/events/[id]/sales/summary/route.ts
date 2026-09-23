import { cookies } from "next/headers";

import { proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
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
  return proxyJSONRead(request, `/api/v1/staff/events/${id}/sales/summary`, token);
}
