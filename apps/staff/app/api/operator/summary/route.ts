import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorSummary } from "@/lib/operator-api";
import { proxyRead } from "@/lib/reader-abort";
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

  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<OperatorSummary>("/api/v1/operator/summary", {
        method: "GET",
        sessionToken: token,
        signal,
      });
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
