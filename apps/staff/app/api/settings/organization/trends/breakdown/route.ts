import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OrganizationTrendBreakdown } from "@/lib/organization-trends-breakdown";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The breakdown of Organization Trends (#864, ADR 0081): the Events one
// bucket of one figure was made of. Any Member may read it; the Go API
// refuses Takings to Event Staff with a 403 and a bucket it does not state
// with a 400, both forwarded verbatim for the panel to say. The query names
// the figure and the bucket, forwarded as it came.
const BACKEND_PATH = "/api/v1/staff/organization/trends/breakdown";

export async function GET(request: Request) {
  const token = (await cookies()).get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<OrganizationTrendBreakdown>(`${BACKEND_PATH}${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
