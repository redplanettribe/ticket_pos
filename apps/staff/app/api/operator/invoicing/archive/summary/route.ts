import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import type { TaxDocumentArchiveSummary } from "@/lib/tax-document-archive";

// GET returns what the Tax Document Archive of a range would hold (#631):
// its facturas and Credit Notes, and the unsettled documents it leaves out,
// passing `from` and `to` on exactly as the archive's own proxy does. A
// refusal is the API's envelope, forwarded as is.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const incoming = new URL(request.url).searchParams;
  const params = new URLSearchParams({ from: incoming.get("from") ?? "", to: incoming.get("to") ?? "" });
  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<TaxDocumentArchiveSummary>(
        `/api/v1/operator/invoicing/archive/summary?${params.toString()}`,
        { method: "GET", sessionToken: token, signal },
      );
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
