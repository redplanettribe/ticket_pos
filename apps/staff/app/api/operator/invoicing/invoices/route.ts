import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail, OperatorInvoiceListPage } from "@/lib/operator-api";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The Tax Invoices (#454, ADR 0059). The Go API is the gate: a session whose
// email is not on the platform operator allowlist gets a 403 both verbs
// forward verbatim. GET lists newest first in the ADR-0006 nested envelope;
// POST issues one and answers with it as it stands after the SRI replied —
// authorized, not authorized, rejected or pending — the SRI's messages being
// structured data on the invoice, never an HTTP error.
const BACKEND_PATH = "/api/v1/operator/invoicing/invoices";

export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const search = new URL(request.url).search;
  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<OperatorInvoiceListPage>(`${BACKEND_PATH}${search}`, {
        method: "GET",
        sessionToken: token,
        signal,
      });
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}

export async function POST(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorInvoiceDetail>(BACKEND_PATH, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
