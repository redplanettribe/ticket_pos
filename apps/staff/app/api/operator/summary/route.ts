import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorSummary } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns platform revenue totals grouped by currency. The Go API is the
// gate: a session whose email is not on the platform operator allowlist gets a
// 403 that this route forwards verbatim.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorSummary>("/api/v1/operator/summary", {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
