import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";

/**
 * Forwards a Tax Invoice document download from the Go API as-is (#456): an
 * error is the API's JSON envelope passed through unchanged so the caller can
 * show the reason; a success keeps the content type and the filename the API
 * chose, so the browser saves the file under the clave. The same shape as the
 * Sales Export proxy.
 */
export async function proxyInvoiceDocument(path: string, token: string): Promise<NextResponse> {
  const upstream = await fetchBackendRaw(path, { method: "GET", sessionToken: token });
  if (!upstream.ok) {
    const body = await upstream.text();
    return new NextResponse(body, {
      status: upstream.status,
      headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
    });
  }
  const headers = new Headers();
  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? "application/xml; charset=utf-8");
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
