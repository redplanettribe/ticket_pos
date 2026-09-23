import { cookies } from "next/headers";

import { proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The documents that need attention (#477, ADR 0060): every document parked
// needs_attention, longest waiting first, in the ADR-0006 nested envelope.
// The Go API is the gate; a session off the operator allowlist gets the 403
// forwarded verbatim.
const BACKEND_PATH = "/api/v1/operator/invoicing/needs-attention";

export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  return proxyJSONRead(request, `${BACKEND_PATH}${search}`, token);
}
