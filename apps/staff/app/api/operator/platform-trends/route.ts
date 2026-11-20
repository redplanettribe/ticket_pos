import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { PlatformTrends } from "@/lib/platform-trends";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// Platform Trends (#821, ADR 0080): the figures for a span with a comparison.
// The Go API is the gate: a session that is not a Platform Operator's gets a
// 403 this route forwards verbatim, and a span it refuses (a custom range the
// wrong way round) a 400 whose message the section shows. The query is the
// reading's address, forwarded as it came.
const BACKEND_PATH = "/api/v1/operator/platform-trends";

export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<PlatformTrends>(`${BACKEND_PATH}${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
