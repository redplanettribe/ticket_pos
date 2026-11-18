import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { PlatformTrendBreakdown } from "@/lib/platform-trends-breakdown";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The breakdown of Platform Trends (#822, ADR 0080): what one bucket of one
// figure was made of. The Go API is the gate: a session that is not a
// Platform Operator's gets a 403 this route forwards verbatim, and a bucket
// it refuses a 400 whose message the panel shows. The query names the figure
// and the bucket, forwarded as it came.
const BACKEND_PATH = "/api/v1/operator/platform-trends/breakdown";

export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<PlatformTrendBreakdown>(`${BACKEND_PATH}${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
