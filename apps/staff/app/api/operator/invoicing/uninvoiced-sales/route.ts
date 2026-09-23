import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorUninvoicedHouseSalePage } from "@/lib/operator-api";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET lists the Uninvoiced House Sales platform-wide, oldest sale first, in
// the ADR-0006 nested envelope (#507, ADR 0064). The Go API is the gate: a
// session whose email is not on the platform operator allowlist gets a 403
// forwarded verbatim, and it answers 404 SALE_INVOICING_UNAVAILABLE while
// the feature is closed, forwarded as is. The page and page size ride the
// query string through.
const BACKEND_PATH = "/api/v1/operator/invoicing/uninvoiced-sales";

export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<OperatorUninvoicedHouseSalePage>(`${BACKEND_PATH}${search}`, {
        method: "GET",
        sessionToken: token,
        signal,
      });
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
