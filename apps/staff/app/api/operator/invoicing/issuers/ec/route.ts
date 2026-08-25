import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorEcuadorIssuer } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The Ecuador Issuer (#451, ADR 0059): the platform's own registration with
// the SRI. The Go API is the gate — a session whose email is not on the
// platform operator allowlist gets a 403 that both routes forward verbatim —
// and the country code in the path is the API's own seam, mirrored here so a
// second country is a second route beside this one.
const BACKEND_PATH = "/api/v1/operator/invoicing/issuers/ec";

// GET returns the Issuer, or `null` data when none has been recorded yet.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorEcuadorIssuer | null>(BACKEND_PATH, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// PUT records the Issuer: created on the first save, replaced after. The body
// is forwarded unread; the API validates every field and names each failing
// one, and this route forwards that answer as it is.
export async function PUT(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorEcuadorIssuer>(BACKEND_PATH, {
      method: "PUT",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
