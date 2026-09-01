import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// Issue again (#580, ADR 0068): owe the Ticket Sale a fresh Sale Invoice to
// replace a terminally dead one — abandoned, or annulled at the portal — and
// answer with the REPLACEMENT, a new document, owed and unsigned. The
// optional note goes through; who pressed is the session's, and the Go API
// takes it from there. The refusals — a manual document, a Credit Note, a
// document that is not terminally dead, a Sale that no longer stands, a live
// replacement already standing — are forwarded by code.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorInvoiceDetail>(
      `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/issue-again`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
