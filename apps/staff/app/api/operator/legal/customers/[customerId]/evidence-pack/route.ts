import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ customerId: string }>;
};

// One Customer's Consent Evidence Pack (#568), streamed through the BFF.
//
// A BLOB PROXY IN THE HOLDER EXPORT'S SHAPE, deliberately: two blob proxies
// that differed would differ in which errors reach the reader. The download
// headers are preserved so the browser saves the file under the name the API
// chose — which is keyed on the pack's own SHA-256 and is therefore checkable
// against the bytes (ADR 0067), a property a name invented in this layer would
// destroy.
//
// NOTHING IS PARSED HERE. The pack is a ZIP whose whole value is that it is
// byte-identical to the one the API built; a layer that touched it would be a
// layer that could change what was handed over.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { customerId } = await context.params;
  const upstream = await fetchBackendRaw(
    `/api/v1/operator/legal/customers/${encodeURIComponent(customerId)}/evidence-pack`,
    { method: "GET", sessionToken: token },
  );

  if (!upstream.ok) {
    // The API returns a JSON envelope on error; pass it through unchanged so
    // the operator sees the reason rather than a broken download.
    const body = await upstream.text();
    return new NextResponse(body, {
      status: upstream.status,
      headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
    });
  }

  const headers = new Headers();
  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? "application/zip");
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
