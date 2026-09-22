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

// GET streams the Holder Export .xlsx from the Go API through the BFF,
// preserving the download headers so the browser saves the file — the same shape
// as the Sales Export proxy next door, and deliberately so: two blob proxies
// that differed would differ in which errors reach the reader.
//
// The Holder List's filter query is forwarded VERBATIM rather than picked apart
// here: the API parses it with the Holder List's own parser, and a second reading
// of it in this layer would be a second chance for the file and the screen to
// disagree about which view was downloaded.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const query = new URL(request.url).search;
  const upstream = await fetchBackendRaw(
    `/api/v1/staff/events/${id}/holder-list/export${query}`,
    { method: "GET", sessionToken: token },
  );

  if (!upstream.ok) {
    // The API returns a JSON envelope on error; pass it through unchanged so the
    // caller can show the reason rather than a broken download, and read the
    // code the catalog words it by.
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
  // THE BODY IS PASSED THROUGH AS A STREAM AND NEVER BUFFERED (ADR 0075). The
  // export has no size limit, so buffering it here would put the whole roster
  // in this process's memory. And it is what carries a failure through: when the
  // API aborts a download part way, the upstream body errors, Next's pipe
  // aborts this response with it, and the browser sees a broken connection
  // rather than a clean end - so its blob() rejects and no file is saved.
  return new NextResponse(upstream.body, { status: 200, headers });
}
