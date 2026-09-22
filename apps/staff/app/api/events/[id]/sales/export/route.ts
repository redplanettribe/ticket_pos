import { cookies } from "next/headers";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { proxyDownload, XLSX_CONTENT_TYPE } from "@/lib/download-proxy";
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
  // The same pass-through as the Holder Export: a refusal is the API's envelope
  // with its Retry-After and Cache-Control, a file is streamed unbuffered.
  return proxyDownload(
    request,
    (signal) =>
      fetchBackendRaw(`/api/v1/staff/events/${id}/sales/export${query}`, {
        method: "GET",
        sessionToken: token,
        signal,
      }),
    XLSX_CONTENT_TYPE,
  );
}
