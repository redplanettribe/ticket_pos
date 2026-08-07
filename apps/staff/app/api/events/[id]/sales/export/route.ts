import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// GET streams the Sales Export .xlsx from the Go API through the BFF, preserving
// the download headers so the browser saves the file — the same shape as the
// Sale Import template proxy next door.
//
// The Sales list's filter query is forwarded verbatim rather than picked apart
// here: the API validates it with the Sales list's own parser, and a second
// reading of it in this layer would be a second chance for the file and the
// screen to disagree.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const query = new URL(request.url).search;
  const upstream = await fetchBackendRaw(`/api/v1/staff/events/${id}/sales/export${query}`, {
    method: "GET",
    sessionToken: token,
  });

  if (!upstream.ok) {
    // The API returns a JSON envelope on error; pass it through unchanged so
    // the caller can show the reason rather than a broken download.
    const body = await upstream.text();
    return new NextResponse(body, {
      status: upstream.status,
      headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
    });
  }

  const headers = new Headers();
  headers.set(
    "Content-Type",
    upstream.headers.get("Content-Type") ??
      "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  );
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
