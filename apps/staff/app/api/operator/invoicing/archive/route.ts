import { cookies } from "next/headers";

import { unauthorizedResponse } from "@/lib/bff";
import { proxyInvoiceDocument } from "@/lib/invoice-document-proxy";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET streams the Tax Document Archive from the Go API through the BFF (#630),
// passing `from` and `to` on and keeping the download headers, so the browser
// saves the ZIP under the name the API chose. The body is streamed, never
// buffered here. A refusal is the API's envelope, passed through.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const incoming = new URL(request.url).searchParams;
  const params = new URLSearchParams({ from: incoming.get("from") ?? "", to: incoming.get("to") ?? "" });
  return proxyInvoiceDocument(request, `/api/v1/operator/invoicing/archive?${params.toString()}`, token);
}
