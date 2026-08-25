import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The Ecuador Issuer's signing certificate (#453, ADR 0059). The multipart
// body — the .p12 and its password — is forwarded to the Go API as the
// browser built it and the API's answer comes back as it is, status and all:
// the API opens the file, decides which of its codes applies, and keeps the
// bytes encrypted. Nothing here reads the file, and nothing here could put it
// anywhere else — the buckets are public, so a presigned upload was never an
// option for a private key.
const BACKEND_PATH = "/api/v1/operator/invoicing/issuers/ec/certificate";

export async function POST(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const form = await request.formData();
  const upstream = await fetchBackendRaw(BACKEND_PATH, {
    method: "POST",
    sessionToken: token,
    body: form,
  });

  const body = await upstream.text();
  return new NextResponse(body, {
    status: upstream.status,
    headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
  });
}
