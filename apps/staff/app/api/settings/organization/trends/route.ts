import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OrganizationTrends } from "@/lib/organization-trends";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// Organization Trends (#861, ADR 0081): the Active Member's Organization's
// figures for a span with a comparison. Any Member may read it; a span the
// API refuses (a custom range the wrong way round) comes back as a 400 whose
// message the section shows. The query is the reading's address, forwarded
// as it came.
const BACKEND_PATH = "/api/v1/staff/organization/trends";

export async function GET(request: Request) {
  const token = (await cookies()).get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<OrganizationTrends>(`${BACKEND_PATH}${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
