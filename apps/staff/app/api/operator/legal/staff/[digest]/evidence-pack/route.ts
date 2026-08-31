import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ digest: string }>;
};

// One staff person's Consent Evidence Pack (#568), keyed on their Staff Digest.
//
// THE SAME FILE THE CUSTOMER ROUTE SERVES where the address is also a
// Customer's: a pack spans both populations, and one access request has one
// answer. The digest is forwarded and never decoded here — it is one-way by
// design, and this layer has no key and needs none.
//
// The digest names the path and NOTHING ELSE: it appears nowhere in the file it
// fetches, nor in that file's name (#548), so a key rotation costs a bookmark
// rather than orphaning a document.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { digest } = await context.params;
  const upstream = await fetchBackendRaw(
    `/api/v1/operator/legal/staff/${encodeURIComponent(digest)}/evidence-pack`,
    { method: "GET", sessionToken: token },
  );

  if (!upstream.ok) {
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
