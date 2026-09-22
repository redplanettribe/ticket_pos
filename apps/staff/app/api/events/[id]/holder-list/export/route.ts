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

// GET streams the Holder Export .xlsx from the Go API through the BFF,
// preserving the download headers so the browser saves the file — the same shape
// as the Sales Export proxy next door, and deliberately so: both go through
// lib/download-proxy, because two blob proxies that differed would differ in
// which errors reach the reader.
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
  // A refusal is the API's envelope with its Retry-After and Cache-Control, so
  // the caller can show the reason, read the code the catalog words it by, and
  // know when a busy export will take it. A file is streamed and never buffered
  // (ADR 0075): see forwardDownload for why that is also what carries a failure
  // part way through to the reader.
  return proxyDownload(
    request,
    (signal) =>
      fetchBackendRaw(`/api/v1/staff/events/${id}/holder-list/export${query}`, {
        method: "GET",
        sessionToken: token,
        signal,
      }),
    XLSX_CONTENT_TYPE,
  );
}
