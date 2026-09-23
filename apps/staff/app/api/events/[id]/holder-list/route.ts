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

// The Event's Holder List (#333; the Outstanding Answers read of #313, widened
// to the roster): every Ticket of the Event, who is coming on each, and — where
// the Event asks questions — what each still owes.
//
// NAMED AFTER THE LIST, AT BOTH ENDS (#519, ADR 0065). This route and the API
// path it calls were both `outstanding-answers` until `outstanding` became one
// filter of seven. The API still answers on the old path for one release
// against deploy skew, and this BFF deliberately does NOT use it: an alias
// exists so that an OLD frontend can reach a NEW backend, and a new frontend
// calling the old path would keep the shim alive past the release that deletes
// it (#531).
//
// The paging parameters — and the `outstanding` filter — are FORWARDED AND NOT
// PARSED. The API floors, clamps and defaults them itself, and a second reading
// of them here could only ever disagree with the first — the BFF's job on this
// route is to attach the session and get out of the way.
//
// The reader's abort signal goes upstream with the session (proxyJSONRead): a
// Holder List page can be a slow read, and a reader who has left should not
// keep the API working on it.
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

  return proxyJSONRead(request, `/api/v1/staff/events/${id}/holder-list${suffix}`, token);
}
