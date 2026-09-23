import { cookies } from "next/headers";

import { proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns a page of every Organization on the platform with its signed
// Withdrawable Balance (ADR-0006 nested { data, pagination } envelope),
// forwarding the pagination query string to the Go API.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const search = new URL(request.url).search;
  return proxyJSONRead(request, `/api/v1/operator/organizations${search}`, token);
}
