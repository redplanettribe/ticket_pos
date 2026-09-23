import { cookies } from "next/headers";

import { proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns platform revenue totals grouped by currency. The Go API is the
// gate: a session whose email is not on the platform operator allowlist gets a
// 403 that this route forwards verbatim.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  return proxyJSONRead(request, "/api/v1/operator/summary", token);
}
