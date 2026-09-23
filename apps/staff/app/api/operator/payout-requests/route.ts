import { cookies } from "next/headers";

import { proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns the cross-organization queue of outstanding payout requests,
// oldest first, in the ADR-0006 nested { data, pagination } envelope, forwarding
// the pagination query string to the Go API. Account numbers arrive masked from
// the API — this proxy never sees a whole one.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const search = new URL(request.url).search;
  return proxyJSONRead(request, `/api/v1/operator/payout-requests${search}`, token);
}
